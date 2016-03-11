package scheduler

import (
	"container/list"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
)

// Scheduler assigns tasks to nodes.
type Scheduler struct {
	store           state.WatchableStore
	unassignedTasks *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// New creates a new scheduler.
func New(store state.WatchableStore) *Scheduler {
	return &Scheduler{
		store:           store,
		unassignedTasks: list.New(),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}
}

func (s *Scheduler) setupTasksList(tx state.ReadTx) error {
	tasks, err := tx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID == "" {
			log.Infof("Queueing %#v", t)
			s.enqueue(t)
		}
	}

	return nil
}

// Run is the scheduler event loop.
func (s *Scheduler) Run() error {
	defer close(s.doneChan)

	updates := s.store.WatchQueue().Watch()
	defer s.store.WatchQueue().StopWatch(updates)

	err := s.store.View(s.setupTasksList)
	if err != nil {
		log.Errorf("could not snapshot store: %v", err)
		return err
	}

	// Queue all unassigned tasks before processing changes.
	s.tick()

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.Payload.(type) {
			case state.EventCreateTask:
				pendingChanges += s.createTask(v.Task)
			case state.EventUpdateTask:
				pendingChanges += s.createTask(v.Task)
			case state.EventCreateNode:
				if v.Node.Status.State == api.NodeStatus_READY {
					pendingChanges++
				}
			case state.EventUpdateNode:
				if v.Node.Status.State == api.NodeStatus_READY {
					pendingChanges++
				}
			case state.EventCommit:
				if pendingChanges > 0 {
					s.tick()
					pendingChanges = 0
				}
			}
		case <-s.stopChan:
			return nil
		}
	}
}

// Stop causes the scheduler event loop to stop running.
func (s *Scheduler) Stop() {
	close(s.stopChan)
	<-s.doneChan
}

// enqueue queues a task for scheduling.
func (s *Scheduler) enqueue(t *api.Task) {
	s.unassignedTasks.PushBack(t)
}

func (s *Scheduler) createTask(t *api.Task) int {
	if t.NodeID == "" {
		// unassigned task
		s.enqueue(t)
		return 1
	}
	return 0
}

// tick attempts to schedule the queue.
func (s *Scheduler) tick() {
	nextBatch := list.New()

	// TODO(aaronl): Ideally, we would make scheduling decisions outside
	// of an Update callback, since Update blocks other writes to the
	// store. The current approach of making the decisions inside Update
	// is done to keep the store simple. Eventually, we may want to break
	// this up into a View where the decisions are made, and an Update that
	// applies them. This will require keeping local state to keep track of
	// allocations as they are made, since the store itself can't be
	// changed through View.
	err := s.store.Update(func(tx state.Tx) error {
		nodes, err := tx.Nodes().Find(state.All)
		if err != nil {
			return err
		}

		var next *list.Element
		for e := s.unassignedTasks.Front(); e != nil; e = next {
			next = e.Next()
			t := e.Value.(*api.Task)
			if newT := s.scheduleTask(tx, nodes, *t); newT == nil {
				// scheduling failed; keep this task in the list
				nextBatch.PushBack(t)
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf("Error in transaction: %v", err)

		// leave unassignedTasks list in place
	} else {
		s.unassignedTasks = nextBatch
	}
}

// scheduleTask schedules a single task.
func (s *Scheduler) scheduleTask(tx state.Tx, nodes []*api.Node, t api.Task) *api.Task {
	node := s.selectNodeForTask(tx, nodes, &t)
	if node == nil {
		log.Info("No nodes available to assign tasks to")
		return nil
	}

	log.Infof("Assigning task %s to node %s", t.ID, node.ID)
	t.NodeID = node.ID
	t.Status = &api.TaskStatus{State: api.TaskStateAssigned}
	if err := tx.Tasks().Update(&t); err != nil {
		log.Error(err)
		return nil
	}
	return &t
}

// selectNodeForTask is a naive scheduler. Will select a ready, non-drained
// node with the fewer number of tasks already running.
func (s *Scheduler) selectNodeForTask(tx state.Tx, nodes []*api.Node, t *api.Task) *api.Node {
	var target *api.Node
	targetTasks := 0

	for _, n := range nodes {
		if n.Status.State != api.NodeStatus_READY {
			continue
		}

		tasks, err := tx.Tasks().Find(state.ByNodeID(n.ID))
		if err != nil {
			log.Errorf("Error selecting tasks by node: %v", err)
			continue
		}
		nodeTasks := len(tasks)
		if target == nil || nodeTasks < targetTasks {
			target = n
			targetTasks = nodeTasks
		}
	}

	return target
}
