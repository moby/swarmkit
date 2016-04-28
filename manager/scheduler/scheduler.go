package scheduler

import (
	"container/heap"
	"container/list"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
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
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}
}

func (s *Scheduler) setupTasksList(tx state.ReadTx) error {
	tasks, err := tx.Tasks().Find(state.All)
	if err != nil {
		return err
	}

	tasksByNode := make(map[string]map[string]*api.Task)
	for _, t := range tasks {
		// Ignore all tasks that have not reached ALLOCATED
		// state.
		if t.Status.State < api.TaskStateAllocated {
			continue
		}

		s.allTasks[t.ID] = t
		if t.NodeID == "" {
			s.enqueue(t)
		} else {
			if tasksByNode[t.NodeID] == nil {
				tasksByNode[t.NodeID] = make(map[string]*api.Task)
			}
			tasksByNode[t.NodeID][t.ID] = t
		}
	}

	if err := s.buildNodeHeap(tx, tasksByNode); err != nil {
		return err
	}

	return nil
}

// Run is the scheduler event loop.
func (s *Scheduler) Run(ctx context.Context) error {
	defer close(s.doneChan)

	updates, cancel, err := state.ViewAndWatch(s.store, s.setupTasksList)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("snapshot store update failed")
		return err
	}
	defer cancel()

	// Queue all unassigned tasks before processing changes.
	s.tick(ctx)

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.(type) {
			case state.EventCreateTask:
				pendingChanges += s.createTask(ctx, v.Task)
			case state.EventUpdateTask:
				pendingChanges += s.updateTask(ctx, v.Task)
			case state.EventDeleteTask:
				s.deleteTask(ctx, v.Task)
			case state.EventCreateNode:
				s.createOrUpdateNode(v.Node)
				pendingChanges++
			case state.EventUpdateNode:
				s.createOrUpdateNode(v.Node)
				pendingChanges++
			case state.EventDeleteNode:
				s.nodeHeap.remove(v.Node.ID)
			case state.EventCommit:
				if pendingChanges > 0 {
					s.tick(ctx)
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

func (s *Scheduler) createTask(ctx context.Context, t *api.Task) int {
	// Ignore all tasks that have not reached ALLOCATED
	// state.
	if t.Status.State < api.TaskStateAllocated {
		return 0
	}

	s.allTasks[t.ID] = t
	if t.NodeID == "" {
		// unassigned task
		s.enqueue(t)
		return 1
	}

	nodeInfo := s.nodeHeap.nodeInfo(t.NodeID)
	nodeInfo.addTask(t)
	s.nodeHeap.updateNode(nodeInfo)

	return 0
}

func (s *Scheduler) updateTask(ctx context.Context, t *api.Task) int {
	// Ignore all tasks that have not reached ALLOCATED
	// state.
	if t.Status.State < api.TaskStateAllocated {
		return 0
	}

	oldTask := s.allTasks[t.ID]
	if oldTask != nil {
		s.deleteTask(ctx, oldTask)
	}
	return s.createTask(ctx, t)
}

func (s *Scheduler) deleteTask(ctx context.Context, t *api.Task) {
	delete(s.allTasks, t.ID)
	nodeInfo := s.nodeHeap.nodeInfo(t.NodeID)
	nodeInfo.removeTask(t)
	s.nodeHeap.updateNode(nodeInfo)
}

func (s *Scheduler) createOrUpdateNode(n *api.Node) {
	var resources api.Resources
	if n.Description != nil && n.Description.Resources != nil {
		resources = *n.Description.Resources
	}
	s.nodeHeap.addOrUpdateNode(newNodeInfo(n, map[string]*api.Task{}, resources))
}

// tick attempts to schedule the queue.
func (s *Scheduler) tick(ctx context.Context) {
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
		if newT := s.scheduleTask(ctx, t); newT != nil {
			schedulingDecisions[id] = schedulingDecision{old: t, new: newT}
			s.unassignedTasks.Remove(e)
		}
	}

	var failedSchedulingDecisions []schedulingDecision
	schedulingDecisionsSlice := make([]schedulingDecision, 0, len(schedulingDecisions))

	for _, decision := range schedulingDecisions {
		schedulingDecisionsSlice = append(schedulingDecisionsSlice, decision)
	}

	// Apply changes to master store
	applied, err := s.store.Batch(func(batch state.Batch) error {
		for _, decision := range schedulingDecisionsSlice {
			err := batch.Update(func(tx state.Tx) error {
				t := tx.Tasks().Get(decision.old.ID)
				if t == nil {
					// Task no longer exists. Do nothing.
					return nil
				}

				return tx.Tasks().Update(decision.new)
			})
			if err != nil {
				log.G(ctx).Debugf("scheduler failed to update task %s; will retry", decision.old.ID)
				failedSchedulingDecisions = append(failedSchedulingDecisions, decision)
			}
		}
		return nil
	})

	if err != nil {
		log.G(ctx).WithError(err).Error("scheduler tick transaction failed")

		s.rollbackLocalState(schedulingDecisionsSlice[applied:])
		return
	}
	s.rollbackLocalState(failedSchedulingDecisions)
}

func (s *Scheduler) rollbackLocalState(decisions []schedulingDecision) {
	for _, decision := range decisions {
		s.allTasks[decision.old.ID] = decision.old

		nodeInfo := s.nodeHeap.nodeInfo(decision.new.NodeID)
		nodeInfo.removeTask(decision.new)
		s.nodeHeap.updateNode(nodeInfo)

		s.enqueue(decision.old)
	}
}

// scheduleTask schedules a single task.
func (s *Scheduler) scheduleTask(ctx context.Context, t *api.Task) *api.Task {
	pipeline := NewPipeline(t)
	n, _ := s.nodeHeap.findMin(pipeline.Process, s.scanAllNodes)
	if n == nil {
		log.G(ctx).WithField("task.id", t.ID).Debug("No nodes available to assign tasks to")
		return nil
	}

	log.G(ctx).WithField("task.id", t.ID).Debugf("Assigning to node %s", n.ID)
	newT := *t
	newT.NodeID = n.ID
	newT.Status = api.TaskStatus{State: api.TaskStateAssigned}
	s.allTasks[t.ID] = &newT

	nodeInfo := s.nodeHeap.nodeInfo(n.ID)
	nodeInfo.addTask(&newT)
	s.nodeHeap.updateNode(nodeInfo)
	return &newT
}

func (s *Scheduler) buildNodeHeap(tx state.ReadTx, tasksByNode map[string]map[string]*api.Task) error {
	nodes, err := tx.Nodes().Find(state.All)
	if err != nil {
		return err
	}

	s.nodeHeap.alloc(len(nodes))

	i := 0
	for _, n := range nodes {
		var resources api.Resources
		if n.Description != nil && n.Description.Resources != nil {
			resources = *n.Description.Resources
		}
		s.nodeHeap.heap = append(s.nodeHeap.heap, newNodeInfo(n, tasksByNode[n.ID], resources))
		s.nodeHeap.index[n.ID] = i
		i++
	}

	heap.Init(&s.nodeHeap)

	return nil
}
