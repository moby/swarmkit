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
	watcher     chan events.Event
	cancelWatch func()
	dirty       map[instanceTuple]struct{}

	// List of tasks collected for cleanup, which includes two kinds of tasks
	// - serviceless orphaned tasks
	// - tasks with desired state REMOVE that have already been shut down
	cleanup  []string
	stopChan chan struct{}
	doneChan chan struct{}
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

	var orphanedTasks []*api.Task
	var removeTasks []*api.Task
	tr.store.View(func(readTx store.ReadTx) {
		var err error

		clusters, err := store.FindClusters(readTx, store.ByName(store.DefaultClusterName))
		if err == nil && len(clusters) == 1 {
			tr.taskHistory = clusters[0].Spec.Orchestration.TaskHistoryRetentionLimit
		}

		// On startup, scan the entire store and inspect orphaned tasks from previous life.
		orphanedTasks, err = store.FindTasks(readTx, store.ByTaskState(api.TaskStateOrphaned))
		if err != nil {
			log.G(context.TODO()).WithError(err).Error("failed to find Orphaned tasks in task reaper init")
		}
		removeTasks, err = store.FindTasks(readTx, store.ByDesiredState(api.TaskStateRemove))
		if err != nil {
			log.G(context.TODO()).WithError(err).Error("failed to find tasks with desired state REMOVE in task reaper init")
		}
	})

	if len(orphanedTasks)+len(removeTasks) > 0 {
		for _, t := range orphanedTasks {
			// Do not reap service tasks immediately.
			// Let them go through the regular history cleanup process
			// of checking TaskHistoryRetentionLimit.
			if t.ServiceID != "" {
				continue
			}

			// Serviceless tasks can be cleaned up right away since they are not attached to a service.
			tr.cleanup = append(tr.cleanup, t.ID)
		}
		// tasks with desired state REMOVE that have progressed beyond COMPLETE or
		// haven't been assigned yet can be cleaned up right away
		for _, t := range removeTasks {
			if t.Status.State < api.TaskStateAssigned || t.Status.State >= api.TaskStateCompleted {
				tr.cleanup = append(tr.cleanup, t.ID)
			}
		}
		// Clean up tasks in 'cleanup' right away
		if len(tr.cleanup) > 0 {
			tr.tick()
		}
	}

	timer := time.NewTimer(reaperBatchingInterval)

	// Watch for:
	// 1. EventCreateTask for cleaning slots, which is the best time to cleanup that node/slot.
	// 2. EventUpdateTask for cleaning
	//    - serviceless orphaned tasks (when orchestrator updates the task status to ORPHANED)
	//    - tasks which have desired state REMOVE and have been shut down by the agent
	//      (these are tasks which are associated with slots removed as part of service
	//       remove or scale down)
	// 3. EventUpdateCluster for TaskHistoryRetentionLimit update.
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
				// add serviceless orphaned tasks
				if t.Status.State >= api.TaskStateOrphaned && t.ServiceID == "" {
					tr.cleanup = append(tr.cleanup, t.ID)
				}
				// add tasks that are yet unassigned or have progressed beyond COMPLETE, with
				// desired state REMOVE. These tasks are associated with slots that were removed
				// as part of a service scale down or service removal.
				if t.DesiredState == api.TaskStateRemove && (t.Status.State < api.TaskStateAssigned || t.Status.State >= api.TaskStateCompleted) {
					tr.cleanup = append(tr.cleanup, t.ID)
				}
			case api.EventUpdateCluster:
				tr.taskHistory = v.Cluster.Spec.Orchestration.TaskHistoryRetentionLimit
			}

			if len(tr.dirty)+len(tr.cleanup) > maxDirty {
				timer.Stop()
				tr.tick()
			} else {
				timer.Reset(reaperBatchingInterval)
			}
		case <-timer.C:
			timer.Stop()
			tr.tick()
		case <-tr.stopChan:
			timer.Stop()
			return
		}
	}
}

func (tr *TaskReaper) tick() {
	if len(tr.dirty) == 0 && len(tr.cleanup) == 0 {
		return
	}

	defer func() {
		tr.cleanup = nil
	}()

	deleteTasks := make(map[string]struct{})
	for _, tID := range tr.cleanup {
		deleteTasks[tID] = struct{}{}
	}
	tr.store.View(func(tx store.ReadTx) {
		for dirty := range tr.dirty {
			service := store.GetService(tx, dirty.serviceID)
			if service == nil {
				continue
			}

			taskHistory := tr.taskHistory

			if taskHistory < 0 {
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
				continue
			}

			// TODO(aaronl): This could filter for non-running tasks and use quickselect
			// instead of sorting the whole slice.
			sort.Sort(tasksByTimestamp(historicTasks))

			runningTasks := 0
			for _, t := range historicTasks {
				// Skip tasks which are desired to be running but the current state
				// is less than or equal to running.
				// This check is important to ignore tasks which are running or need to be running,
				// but to delete tasks which are either past running,
				// or have not reached running but need to be shutdown (because of a service update, for example).
				if t.DesiredState == api.TaskStateRunning && t.Status.State <= api.TaskStateRunning {
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
