package scheduler

import (
	"container/heap"
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
	store           state.WatchableStore
	unassignedTasks *list.List
	nodeHeap        nodeHeap
	allTasks        map[string]*api.Task
	tasksByNode     map[string]map[string]*api.Task

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}

	// This currently exists only for benchmarking. It tells the scheduler
	// scan the whole heap instead of taking the minimum-valued node
	// blindly.
	scanAllNodes bool
}

// New creates a new scheduler.
func New(store state.WatchableStore) *Scheduler {
	return &Scheduler{
		store:           store,
		unassignedTasks: list.New(),
		allTasks:        make(map[string]*api.Task),
		tasksByNode:     make(map[string]map[string]*api.Task),
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
		s.allTasks[t.ID] = t
		if t.NodeID == "" {
			s.enqueue(t)
		} else {
			if s.tasksByNode[t.NodeID] == nil {
				s.tasksByNode[t.NodeID] = make(map[string]*api.Task)
			}
			s.tasksByNode[t.NodeID][t.ID] = t
		}
	}

	if err := s.buildNodeHeap(tx); err != nil {
		return err
	}

	return nil
}

// Run is the scheduler event loop.
func (s *Scheduler) Run() error {
	defer close(s.doneChan)

	updates, cancel, err := state.ViewAndWatch(s.store, s.setupTasksList)
	if err != nil {
		log.Errorf("could not snapshot store: %v", err)
		return err
	}
	defer cancel()

	// Queue all unassigned tasks before processing changes.
	s.tick()

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.(type) {
			case state.EventCreateTask:
				pendingChanges += s.createTask(v.Task)
			case state.EventUpdateTask:
				pendingChanges += s.updateTask(v.Task)
			case state.EventDeleteTask:
				s.deleteTask(v.Task)
			case state.EventCreateNode:
				pendingChanges += s.createNode(v.Node)
			case state.EventUpdateNode:
				pendingChanges += s.updateNode(v.Node)
			case state.EventDeleteNode:
				s.nodeHeap.remove(v.Node.ID)
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

func schedulableNode(n NodeInfo) bool {
	return n.Status.State == api.NodeStatus_READY &&
		(n.Spec == nil || n.Spec.Availability == api.NodeAvailabilityActive)
}

func (s *Scheduler) createTask(t *api.Task) int {
	s.allTasks[t.ID] = t
	if t.NodeID == "" {
		// unassigned task
		s.enqueue(t)
		return 1
	}
	if s.tasksByNode[t.NodeID] == nil {
		s.tasksByNode[t.NodeID] = make(map[string]*api.Task)
	}
	s.tasksByNode[t.NodeID][t.ID] = t

	s.nodeHeap.updateNode(t.NodeID, len(s.tasksByNode[t.NodeID]))

	return 0
}

func (s *Scheduler) updateTask(t *api.Task) int {
	oldTask := s.allTasks[t.ID]
	if oldTask != nil && t.NodeID != oldTask.NodeID {
		if s.tasksByNode[oldTask.NodeID] != nil {
			delete(s.tasksByNode[oldTask.NodeID], oldTask.ID)
			if len(s.tasksByNode[oldTask.NodeID]) == 0 {
				delete(s.tasksByNode, oldTask.NodeID)
			}
			s.nodeHeap.updateNode(oldTask.NodeID, len(s.tasksByNode[oldTask.NodeID]))
		}
		if t.NodeID != "" {
			if s.tasksByNode[t.NodeID] == nil {
				s.tasksByNode[t.NodeID] = make(map[string]*api.Task)
			}
			s.tasksByNode[t.NodeID][t.ID] = t
			s.nodeHeap.updateNode(t.NodeID, len(s.tasksByNode[t.NodeID]))
		}
	}
	s.allTasks[t.ID] = t

	if t.NodeID == "" {
		// unassigned task
		s.enqueue(t)
		return 1
	}
	return 0
}

func (s *Scheduler) deleteTask(t *api.Task) {
	delete(s.allTasks, t.ID)
	if s.tasksByNode[t.NodeID] != nil {
		delete(s.tasksByNode[t.NodeID], t.ID)
		if len(s.tasksByNode[t.NodeID]) == 0 {
			delete(s.tasksByNode, t.NodeID)
		}
		s.nodeHeap.updateNode(t.NodeID, len(s.tasksByNode[t.NodeID]))
	}
}

func (s *Scheduler) createNode(n *api.Node) int {
	s.nodeHeap.addOrUpdateNode(n, len(s.tasksByNode[n.ID]))
	return 1
}

func (s *Scheduler) updateNode(n *api.Node) int {
	s.nodeHeap.addOrUpdateNode(n, len(s.tasksByNode[n.ID]))
	return 1
}

// tick attempts to schedule the queue.
func (s *Scheduler) tick() {
	schedulingDecisions := make(map[string]schedulingDecision, s.unassignedTasks.Len())

	var next *list.Element
	for e := s.unassignedTasks.Front(); e != nil; e = next {
		next = e.Next()
		id := e.Value.(*api.Task).ID
		if _, ok := schedulingDecisions[id]; ok {
			s.unassignedTasks.Remove(e)
			continue
		}
		t := s.allTasks[e.Value.(*api.Task).ID]
		if t == nil || t.NodeID != "" {
			// task deleted or already assigned
			s.unassignedTasks.Remove(e)
			continue
		}
		if newT := s.scheduleTask(t); newT != nil {
			schedulingDecisions[id] = schedulingDecision{old: t, new: newT}
			s.unassignedTasks.Remove(e)
		}
	}

	failedSchedulingDecisions := make(map[string]schedulingDecision)

	// Apply changes to master store
	err := s.store.Update(func(tx state.Tx) error {
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

		s.rollbackLocalState(schedulingDecisions)
		return
	}
	s.rollbackLocalState(failedSchedulingDecisions)
}

func (s *Scheduler) rollbackLocalState(decisions map[string]schedulingDecision) {
	for taskID, decision := range decisions {
		assignedNodeID := decision.new.NodeID

		s.allTasks[decision.old.ID] = decision.old
		delete(s.tasksByNode[assignedNodeID], taskID)
		if len(s.tasksByNode[assignedNodeID]) == 0 {
			delete(s.tasksByNode, assignedNodeID)
		}
		s.nodeHeap.updateNode(assignedNodeID, len(s.tasksByNode[assignedNodeID]))

		s.enqueue(decision.old)
	}
}

// scheduleTask schedules a single task.
func (s *Scheduler) scheduleTask(t *api.Task) *api.Task {
	n, numTasks := s.nodeHeap.findMin(schedulableNode, s.scanAllNodes)
	if n == nil {
		log.WithField("task.id", t.ID).Debug("No nodes available to assign tasks to")
		return nil
	}

	log.WithField("task.id", t.ID).Debugf("Assigning to node %s", n.ID)
	newT := *t
	newT.NodeID = n.ID
	newT.Status = &api.TaskStatus{State: api.TaskStateAssigned}
	s.allTasks[t.ID] = &newT
	if s.tasksByNode[t.NodeID] == nil {
		s.tasksByNode[t.NodeID] = make(map[string]*api.Task)
	}
	s.tasksByNode[t.NodeID][t.ID] = &newT
	s.nodeHeap.updateNode(n.ID, numTasks+1)
	return &newT
}

func (s *Scheduler) buildNodeHeap(tx state.ReadTx) error {
	nodes, err := tx.Nodes().Find(state.All)
	if err != nil {
		return err
	}

	s.nodeHeap.alloc(len(nodes))

	i := 0
	for _, n := range nodes {
		s.nodeHeap.heap = append(s.nodeHeap.heap, newNodeInfo(n, len(s.tasksByNode[n.ID])))
		s.nodeHeap.index[n.ID] = i
		i++
	}

	heap.Init(&s.nodeHeap)

	return nil
}
