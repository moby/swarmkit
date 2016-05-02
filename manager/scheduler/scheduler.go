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
	// preassignedTasks already have NodeID, need resource validation
	preassignedTasks map[string]*api.Task
	nodeHeap         nodeHeap
	allTasks         map[string]*api.Task

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
		store:            store,
		unassignedTasks:  list.New(),
		preassignedTasks: make(map[string]*api.Task),
		allTasks:         make(map[string]*api.Task),
		stopChan:         make(chan struct{}),
		doneChan:         make(chan struct{}),
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
			continue
		}
		// preassigned tasks need to validate resource requirement on corresponding node
		if t.Status.State == api.TaskStateAllocated {
			s.preassignedTasks[t.ID] = t
			continue
		}

		if tasksByNode[t.NodeID] == nil {
			tasksByNode[t.NodeID] = make(map[string]*api.Task)
		}
		tasksByNode[t.NodeID][t.ID] = t
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

	// Validate resource for tasks from preassigned tasks
	// do this before other tasks because preassigned tasks like
	// fill service should start before other tasks
	s.processPreassignedTasks(ctx)

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
				if len(s.preassignedTasks) > 0 {
					s.processPreassignedTasks(ctx)
				}
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
	// state, and tasks that no longer consume resources.
	if t.Status.State < api.TaskStateAllocated || t.Status.State >= api.TaskStateDead {
		return 0
	}

	s.allTasks[t.ID] = t
	if t.NodeID == "" {
		// unassigned task
		s.enqueue(t)
		return 1
	}

	if t.Status.State == api.TaskStateAllocated {
		s.preassignedTasks[t.ID] = t
		// preassigned tasks do not contribute to running tasks count
		return 0
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

	// Ignore tasks that no longer consume any resources.
	if t.Status.State >= api.TaskStateDead {
		return 0
	}

	return s.createTask(ctx, t)
}

func (s *Scheduler) deleteTask(ctx context.Context, t *api.Task) {
	delete(s.allTasks, t.ID)
	delete(s.preassignedTasks, t.ID)
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

func (s *Scheduler) processPreassignedTasks(ctx context.Context) {
	schedulingDecisions := make(map[string]schedulingDecision, len(s.preassignedTasks))
	for _, t := range s.preassignedTasks {
		newT := s.taskFitNode(ctx, t, t.NodeID)
		if newT == nil {
			continue
		}
		schedulingDecisions[t.ID] = schedulingDecision{old: t, new: newT}
	}

	successful, failed := s.applySchedulingDecisions(ctx, schedulingDecisions)

	for _, decision := range successful {
		delete(s.preassignedTasks, decision.old.ID)
	}
	for _, decision := range failed {
		s.allTasks[decision.old.ID] = decision.old
		nodeInfo := s.nodeHeap.nodeInfo(decision.new.NodeID)
		nodeInfo.removeTask(decision.new)
		s.nodeHeap.updateNode(nodeInfo)
	}
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

	_, failed := s.applySchedulingDecisions(ctx, schedulingDecisions)
	for _, decision := range failed {
		s.allTasks[decision.old.ID] = decision.old

		nodeInfo := s.nodeHeap.nodeInfo(decision.new.NodeID)
		nodeInfo.removeTask(decision.new)
		s.nodeHeap.updateNode(nodeInfo)

		// enqueue task for next scheduling attempt
		s.enqueue(decision.old)
	}
}

func (s *Scheduler) applySchedulingDecisions(ctx context.Context, schedulingDecisions map[string]schedulingDecision) (successful, failed []schedulingDecision) {
	if len(schedulingDecisions) == 0 {
		return
	}

	successful = make([]schedulingDecision, 0, len(schedulingDecisions))

	// Apply changes to master store
	applied, err := s.store.Batch(func(batch state.Batch) error {
		for len(schedulingDecisions) > 0 {
			err := batch.Update(func(tx state.Tx) error {
				// Update exactly one task inside this Update
				// callback.
				for taskID, decision := range schedulingDecisions {
					delete(schedulingDecisions, taskID)

					t := tx.Tasks().Get(taskID)
					if t == nil {
						// Task no longer exists. Do nothing.
						failed = append(failed, decision)
						continue
					}

					if err := tx.Tasks().Update(decision.new); err != nil {
						log.G(ctx).Debugf("scheduler failed to update task %s; will retry", taskID)
						failed = append(failed, decision)
						continue
					}
					successful = append(successful, decision)
					return nil
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})

	if err != nil {
		log.G(ctx).WithError(err).Error("scheduler tick transaction failed")
		failed = append(failed, successful[applied:]...)
		successful = successful[:applied]
	}
	return
}

// taskFitNode checks if a node has enough resource to accommodate a task
func (s *Scheduler) taskFitNode(ctx context.Context, t *api.Task, nodeID string) *api.Task {
	nodeInfo := s.nodeHeap.nodeInfo(nodeID)
	pipeline := NewPipeline(t)
	if !pipeline.Process(&nodeInfo) {
		// this node cannot accommodate this task
		return nil
	}
	newT := *t
	newT.Status = api.TaskStatus{State: api.TaskStateAssigned}
	s.allTasks[t.ID] = &newT

	nodeInfo.addTask(&newT)
	s.nodeHeap.updateNode(nodeInfo)
	return &newT
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
