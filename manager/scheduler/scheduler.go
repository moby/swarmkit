package scheduler

import (
	"container/list"
	"reflect"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
)

type schedulingDecision struct {
	old *api.Task
	new *api.Task
}

// Scheduler assigns tasks to nodes.
type Scheduler struct {
	masterStore     state.WatchableStore
	localStore      state.WatchableStore
	unassignedTasks *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// New creates a new scheduler.
func New(store state.WatchableStore) *Scheduler {
	return &Scheduler{
		masterStore:     store,
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
			s.enqueue(t)
		}
	}

	return nil
}

// Run is the scheduler event loop.
func (s *Scheduler) Run() error {
	defer close(s.doneChan)

	s.localStore = state.NewMemoryStore(nil)

	updates, err := state.ViewAndWatch(s.masterStore, s.localStore.CopyFrom)
	if err != nil {
		log.Errorf("could not snapshot store: %v", err)
		return err
	}
	defer s.masterStore.WatchQueue().StopWatch(updates)

	err = s.localStore.View(s.setupTasksList)
	if err != nil {
		log.Errorf("could not set up scheduler tasks list: %v", err)
		return err
	}

	// Queue all unassigned tasks before processing changes.
	s.tick()

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			if err := state.Apply(s.localStore, event); err != nil {
				log.Errorf("scheduler could not apply state change")
			}
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
	schedulingDecisions := make(map[string]schedulingDecision, s.unassignedTasks.Len())

	err := s.localStore.Update(func(tx state.Tx) error {
		nodes, err := tx.Nodes().Find(state.All)
		if err != nil {
			return err
		}

		var next *list.Element
		for e := s.unassignedTasks.Front(); e != nil; e = next {
			next = e.Next()
			id := e.Value.(*api.Task).ID
			if _, ok := schedulingDecisions[id]; ok {
				s.unassignedTasks.Remove(e)
				continue
			}
			t := tx.Tasks().Get(e.Value.(*api.Task).ID)
			if t == nil || t.NodeID != "" {
				// task deleted or already assigned
				s.unassignedTasks.Remove(e)
				continue
			}
			if newT := s.scheduleTask(tx, nodes, *t); newT != nil {
				schedulingDecisions[id] = schedulingDecision{old: t, new: newT}
				s.unassignedTasks.Remove(e)
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf("Error in scheduler local store transaction: %v", err)
		s.readdUnassignedTasks(schedulingDecisions)
		return
	}

	failedSchedulingDecisions := make(map[string]schedulingDecision)

	// Apply changes to master store
	err = s.masterStore.Update(func(tx state.Tx) error {
		for id, decision := range schedulingDecisions {
			t := tx.Tasks().Get(id)
			if t == nil {
				// Task no longer exists
				failedSchedulingDecisions[id] = decision
				continue
			}
			// TODO(aaronl): When we have a sequencer in place,
			// this expensive comparison won't be necessary.
			if !reflect.DeepEqual(t, decision.old) {
				log.Debugf("task changed between scheduling decision and application: %s", t.ID)
				failedSchedulingDecisions[id] = decision
				continue
			}

			if err := tx.Tasks().Update(decision.new); err != nil {
				log.Debugf("scheduler failed to update task %s; will retry", t.ID)
				failedSchedulingDecisions[id] = decision
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf("Error in master store transaction: %v", err)

		s.rollbackLocalStore(schedulingDecisions)
		s.readdUnassignedTasks(schedulingDecisions)
		return
	}
	s.rollbackLocalStore(failedSchedulingDecisions)
	s.readdUnassignedTasks(failedSchedulingDecisions)
}

func (s *Scheduler) rollbackLocalStore(decisions map[string]schedulingDecision) {
	err := s.localStore.Update(func(tx state.Tx) error {
		for _, decision := range decisions {
			if err := tx.Tasks().Update(decision.old); err != nil {
				// Should never fail
				panic("scheduler rollback update failed")
			}
		}
		return nil
	})
	if err != nil {
		// Should never fail
		panic("scheduler rollback failed")
	}
}

func (s *Scheduler) readdUnassignedTasks(decisions map[string]schedulingDecision) {
	for _, decision := range decisions {
		s.enqueue(decision.old)
	}
}

// scheduleTask schedules a single task.
func (s *Scheduler) scheduleTask(tx state.Tx, nodes []*api.Node, t api.Task) *api.Task {
	node := s.selectNodeForTask(tx, nodes, &t)
	if node == nil {
		log.WithField("task.id", t.ID).Debug("No nodes available to assign tasks to")
		return nil
	}

	log.WithField("task.id", t.ID).Debugf("Assigning to node %s", node.ID)
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
		if n.Status.State != api.NodeStatus_READY || (n.Spec != nil && n.Spec.Availability != api.NodeAvailabilityActive) {
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
