package taskreaper

import (
	"sort"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

const (
	// maxDirty is the size threshold for running a task pruning operation.
	maxDirty = 1000
	// reaperBatchingInterval is how often to prune old tasks.
	reaperBatchingInterval = 250 * time.Millisecond
)

type instanceTuple struct {
	instance  uint64 // unset for global tasks
	serviceID string
	nodeID    string // unset for replicated tasks
}

// A TaskReaper deletes old tasks when more than TaskHistoryRetentionLimit tasks
// exist for the same service/instance or service/nodeid combination.
type TaskReaper struct {
	store *store.MemoryStore
	// taskHistory is the number of tasks to keep
	taskHistory int64
	dirty       map[instanceTuple]struct{}
	orphaned    []string
	watcher     chan events.Event
	cancelWatch func()

	stopChan chan struct{}
	doneChan chan struct{}

	// tickSignal is a channel that, if non-nil and available, will be written
	// to to signal that a tick has occurred. its sole purpose is for testing
	// code, to verify that take cleanup attempts are happening when they
	// should be.
	tickSignal chan struct{}
}

// New creates a new TaskReaper.
func New(store *store.MemoryStore) *TaskReaper {
	watcher, cancel := state.Watch(store.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{}, api.EventUpdateCluster{})

	return &TaskReaper{
		store:       store,
		watcher:     watcher,
		cancelWatch: cancel,
		dirty:       make(map[instanceTuple]struct{}),
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}
}

// Run is the TaskReaper's main loop.
func (tr *TaskReaper) Run() {
	defer close(tr.doneChan)

	var tasks []*api.Task
	tr.store.View(func(readTx store.ReadTx) {
		var err error

		clusters, err := store.FindClusters(readTx, store.ByName(store.DefaultClusterName))
		if err == nil && len(clusters) == 1 {
			tr.taskHistory = clusters[0].Spec.Orchestration.TaskHistoryRetentionLimit
		}

		tasks, err = store.FindTasks(readTx, store.ByTaskState(api.TaskStateOrphaned))
		if err != nil {
			log.G(context.TODO()).WithError(err).Error("failed to find Orphaned tasks in task reaper init")
		}
	})

	if len(tasks) > 0 {
		for _, t := range tasks {
			// Do not reap service tasks immediately
			if t.ServiceID != "" {
				continue
			}

			tr.orphaned = append(tr.orphaned, t.ID)
		}

		if len(tr.orphaned) > 0 {
			tr.tick()
		}
	}

	// Clean up when we hit TaskHistoryRetentionLimit or when the timer expires,
	// whichever happens first.
	//
	// Specifically, the way this should work:
	// - Create a timer and immediately stop it. We don't want to fire the
	//   cleanup routine yet, because we just did a cleanup as part of the
	//   initialization above.
	// - Launch into an event loop
	// - When we receive an event, handle the event as needed
	// - After receiving the event:
	//   - If minimum batch size (maxDirty) is exceeded with dirty + cleanup,
	//     then immediately launch into the cleanup routine
	//   - Otherwise, if the timer is stopped, start it (reset).
	// - If the timer expires and the timer channel is signaled, then Stop the
	//   timer (so that it will be ready to be started again as needed), and
	//   execute the cleanup routine (tick)
	timer := time.NewTimer(reaperBatchingInterval)
	timer.Stop()

	// If stop is somehow called AFTER the timer has expired, there will be a
	// value in the timer.C channel. If there is such a value, we should drain
	// it out. This select statement allows us to drain that value if it's
	// present, or continue straight through otherwise.
	select {
	case <-timer.C:
	default:
	}

	// keep track with a boolean of whether the timer is currently stopped
	isTimerStopped := true

	for {
		select {
		case event := <-tr.watcher:
			switch v := event.(type) {
			case api.EventCreateTask:
				t := v.Task
				tr.dirty[instanceTuple{
					instance:  t.Slot,
					serviceID: t.ServiceID,
					nodeID:    t.NodeID,
				}] = struct{}{}
			case api.EventUpdateTask:
				t := v.Task
				if t.Status.State >= api.TaskStateOrphaned && t.ServiceID == "" {
					tr.orphaned = append(tr.orphaned, t.ID)
				}
			case api.EventUpdateCluster:
				tr.taskHistory = v.Cluster.Spec.Orchestration.TaskHistoryRetentionLimit
			}

			if len(tr.dirty)+len(tr.orphaned) > maxDirty {
				// stop the timer, so we don't fire it. if we get another event
				// after we do this cleaning, we will reset the timer then
				timer.Stop()
				// if the timer had fired, drain out the value.
				select {
				case <-timer.C:
				default:
				}
				isTimerStopped = true
				tr.tick()
			} else {
				if isTimerStopped {
					timer.Reset(reaperBatchingInterval)
					isTimerStopped = false
				}
			}
		case <-timer.C:
			// we can safely ignore draining off of the timer channel, because
			// we already know that the timer has expired.
			isTimerStopped = true
			tr.tick()
		case <-tr.stopChan:
			// even though this doesn't really matter in this context, it's
			// good hygiene to drain the value.
			timer.Stop()
			select {
			case <-timer.C:
			default:
			}
			return
		}
	}
}

func (tr *TaskReaper) tick() {
	// this signals that a tick has occurred. it exists solely for testing.
	if tr.tickSignal != nil {
		// try writing to this channel, but if it's full, fall straight through
		// and ignore it.
		select {
		case tr.tickSignal <- struct{}{}:
		default:
		}
	}

	if len(tr.dirty) == 0 && len(tr.orphaned) == 0 {
		return
	}

	defer func() {
		tr.orphaned = nil
	}()

	deleteTasks := make(map[string]struct{})
	for _, tID := range tr.orphaned {
		deleteTasks[tID] = struct{}{}
	}

	// Check history of dirty tasks for cleanup.
	// Note: Clean out the dirty set at the end of this tick iteration
	// in all but one scenarios (documented below).
	// When tick() finishes, the tasks in the slot were either cleaned up,
	// or it was skipped because it didn't meet the criteria for cleaning.
	// Either way, we can discard the dirty set because future events on
	// that slot will cause the task to be readded to the dirty set
	// at that point.
	//
	// The only case when we keep the slot dirty is when there are more
	// than one running tasks present for a given slot.
	// In that case, we need to keep the slot dirty to allow it to be
	// cleaned when tick() is called next and one or more the tasks
	// in that slot have stopped running.
	tr.store.View(func(tx store.ReadTx) {
		for dirty := range tr.dirty {
			service := store.GetService(tx, dirty.serviceID)
			if service == nil {
				delete(tr.dirty, dirty)
				continue
			}

			taskHistory := tr.taskHistory

			if taskHistory < 0 {
				delete(tr.dirty, dirty)
				continue
			}

			var historicTasks []*api.Task

			switch service.Spec.GetMode().(type) {
			case *api.ServiceSpec_Replicated:
				var err error
				historicTasks, err = store.FindTasks(tx, store.BySlot(dirty.serviceID, dirty.instance))
				if err != nil {
					continue
				}

			case *api.ServiceSpec_Global:
				tasksByNode, err := store.FindTasks(tx, store.ByNodeID(dirty.nodeID))
				if err != nil {
					continue
				}

				for _, t := range tasksByNode {
					if t.ServiceID == dirty.serviceID {
						historicTasks = append(historicTasks, t)
					}
				}
			}

			if int64(len(historicTasks)) <= taskHistory {
				delete(tr.dirty, dirty)
				continue
			}

			// TODO(aaronl): This could filter for non-running tasks and use quickselect
			// instead of sorting the whole slice.
			sort.Sort(tasksByTimestamp(historicTasks))

			runningTasks := 0
			for _, t := range historicTasks {
				if t.DesiredState <= api.TaskStateRunning || t.Status.State <= api.TaskStateRunning {
					// Don't delete running tasks
					runningTasks++
					continue
				}

				deleteTasks[t.ID] = struct{}{}

				taskHistory++
				if int64(len(historicTasks)) <= taskHistory {
					break
				}
			}

			// The only case when we keep the slot dirty at the end of tick()
			// is when there are more than one running tasks present
			// for a given slot.
			// In that case, we keep the slot dirty to allow it to be
			// cleaned when tick() is called next and one or more of
			// the tasks in that slot have stopped running.
			if runningTasks <= 1 {
				delete(tr.dirty, dirty)
			}
		}
	})

	if len(deleteTasks) > 0 {
		tr.store.Batch(func(batch *store.Batch) error {
			for taskID := range deleteTasks {
				batch.Update(func(tx store.Tx) error {
					return store.DeleteTask(tx, taskID)
				})
			}
			return nil
		})
	}
}

// Stop stops the TaskReaper and waits for the main loop to exit.
func (tr *TaskReaper) Stop() {
	tr.cancelWatch()
	close(tr.stopChan)
	<-tr.doneChan
}

type tasksByTimestamp []*api.Task

func (t tasksByTimestamp) Len() int {
	return len(t)
}
func (t tasksByTimestamp) Swap(i, j int) {
	t[i], t[j] = t[j], t[i]
}
func (t tasksByTimestamp) Less(i, j int) bool {
	if t[i].Status.Timestamp == nil {
		return true
	}
	if t[j].Status.Timestamp == nil {
		return false
	}
	if t[i].Status.Timestamp.Seconds < t[j].Status.Timestamp.Seconds {
		return true
	}
	if t[i].Status.Timestamp.Seconds > t[j].Status.Timestamp.Seconds {
		return false
	}
	return t[i].Status.Timestamp.Nanos < t[j].Status.Timestamp.Nanos
}
