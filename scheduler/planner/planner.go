package planner

import (
	"container/list"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
)

type store struct {
	readyNodes      map[string]*api.Node
	unassignedTasks *list.List
	tasksByNode     map[string]map[string]*api.Task
	nodeIDByTask    map[string]string
}

func newStore() store {
	return store{
		readyNodes:      make(map[string]*api.Node),
		unassignedTasks: list.New(),
		tasksByNode:     make(map[string]map[string]*api.Task),
		nodeIDByTask:    make(map[string]string),
	}
}

func (s *store) CopyFrom(tx state.ReadTx) error {
	// Clear existing data
	*s = newStore()

	// Copy over new data
	nodes, err := tx.Nodes().Find(state.All)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if n.Status.State == api.NodeStatus_READY {
			s.readyNodes[n.Spec.ID] = n
		}
	}

	tasks, err := tx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID != "" {
			taskMap, ok := s.tasksByNode[t.NodeID]
			if !ok {
				taskMap = make(map[string]*api.Task)
				s.tasksByNode[t.NodeID] = taskMap
			}
			taskMap[t.ID] = t
			s.nodeIDByTask[t.ID] = t.NodeID
		} else {
			log.Infof("Queueing %#v", t)
			s.unassignedTasks.PushBack(t)
		}
	}

	return nil
}

// A Planner assigns tasks to nodes.
type Planner struct {
	masterStore state.WatchableStore
	localStore  store

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// NewPlanner creates a new planner.
func NewPlanner(store state.WatchableStore) (*Planner, error) {
	p := &Planner{
		masterStore: store,
		localStore:  newStore(),
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}

	return p, nil
}

// Run is the planner event loop.
func (p *Planner) Run() error {
	defer close(p.doneChan)

	updates, err := p.masterStore.Snapshot(&p.localStore)
	if err != nil {
		log.Errorf("could not snapshot store: %v", err)
		return err
	}

	// Queue all unassigned tasks before processing changes.
	p.tick()

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event, ok := <-updates:
			if !ok {
				return nil
			}

			switch v := event.Payload.(type) {
			case state.EventCreateTask:
				pendingChanges += p.createTask(v.Task)
			case state.EventUpdateTask:
				p.deleteTask(v.Task)
				pendingChanges += p.createTask(v.Task)
			case state.EventDeleteTask:
				p.deleteTask(v.Task)
			case state.EventCreateNode:
				pendingChanges += p.createNode(v.Node)
			case state.EventUpdateNode:
				// deletion not necessary - just overwrite
				pendingChanges += p.createNode(v.Node)
			case state.EventDeleteNode:
				p.deleteNode(v.Node)
			case state.EventCommit:
				if pendingChanges > 0 {
					p.tick()
					pendingChanges = 0
				}
			}
		case <-p.stopChan:
			p.masterStore.WatchQueue().StopWatch(updates)
			p.stopChan = nil
		}
	}
}

// Stop causes the planner event loop to stop running.
func (p *Planner) Stop() {
	close(p.stopChan)
	<-p.doneChan
}

// enqueue queues a task for scheduling.
func (p *Planner) enqueue(t *api.Task) {
	p.localStore.unassignedTasks.PushBack(t)
}

// deleteTask deletes a task in the local store. This function must be
// idempotent because it's called to alter the local state, and also called
// again when the committed change comes over the watch channel.
func (p *Planner) deleteTask(t *api.Task) {
	nodeID, ok := p.localStore.nodeIDByTask[t.ID]
	if ok {
		if tasksMap, ok := p.localStore.tasksByNode[nodeID]; ok {
			delete(tasksMap, t.ID)
			if len(tasksMap) == 0 {
				delete(p.localStore.tasksByNode, nodeID)
			}
		}
		delete(p.localStore.nodeIDByTask, t.ID)
	}
}

// createTask adds or replaces a task in the local store. This function must be
// idempotent because it's called to alter the local state, and also called
// again when the committed change comes over the watch channel.  Returns the
// number of unassigned tasks added to the queue (either 0 or 1).
func (p *Planner) createTask(t *api.Task) int {
	if t.NodeID != "" {
		taskMap, ok := p.localStore.tasksByNode[t.NodeID]
		if !ok {
			taskMap = make(map[string]*api.Task)
			p.localStore.tasksByNode[t.NodeID] = taskMap
		}
		taskMap[t.ID] = t
		p.localStore.nodeIDByTask[t.ID] = t.NodeID
	} else {
		// unassigned task
		p.enqueue(t)
		return 1
	}
	return 0
}

// deleteNode deletes a node from the local store. This function must be
// idempotent because it's called to alter the local state, and also called
// again when the committed change comes over the watch channel.
func (p *Planner) deleteNode(n *api.Node) {
	delete(p.localStore.readyNodes, n.Spec.ID)
}

// createNode adds or replaces a node in the local store. This function must be
// idempotent because it's called to alter the local state, and also called
// again when the committed change comes over the watch channel. Returns the
// number of ready nodes added (either 0 or 1).
func (p *Planner) createNode(n *api.Node) int {
	if n.Status.State == api.NodeStatus_READY {
		p.localStore.readyNodes[n.Spec.ID] = n
		return 1
	}
	return 0
}

// tick attempts to schedule the queue.
func (p *Planner) tick() {
	nextBatch := list.New()
	type scheduledTask struct {
		old, new *api.Task
	}
	var scheduledTasks []scheduledTask

	err := p.masterStore.Update(func(tx state.Tx) error {
		var next *list.Element
		for e := p.localStore.unassignedTasks.Front(); e != nil; e = next {
			next = e.Next()
			t := e.Value.(*api.Task)
			if newT := p.scheduleTask(tx, *t); newT != nil {
				p.deleteTask(t)
				p.createTask(newT)
				scheduledTasks = append(scheduledTasks, scheduledTask{old: t, new: newT})
			} else {
				// scheduling failed; keep this task in the list
				nextBatch.PushBack(t)
			}
		}
		return nil
	})

	if err != nil {
		log.Errorf("Error in transaction: %v", err)
		// roll back createTask/deleteTask actions
		for _, scheduled := range scheduledTasks {
			p.deleteTask(scheduled.new)
			p.createTask(scheduled.old)
		}

		// leave unassignedTasks list in place
	} else {
		p.localStore.unassignedTasks = nextBatch
	}
}

// scheduleTask schedules a single task.
func (p *Planner) scheduleTask(tx state.Tx, t api.Task) *api.Task {
	node := p.selectNodeForTask(&t, tx)
	if node == nil {
		log.Info("No nodes available to assign tasks to")
		return nil
	}

	// TODO(aaronl): This may assign a task to a node that is no longer
	// eligible if the decision is being made while the planner is not
	// caught up with the latest changes from the snapshot updates channel.
	// The drainer needs to detect this situation and delete tasks that
	// have a bad node assignment. Then the orchestrator will respond to
	// the deletion by creating a new unassigned task, giving the planner
	// another chance at assigning it.
	log.Infof("Assigning task %s to node %s", t.ID, node.Spec.ID)
	t.NodeID = node.Spec.ID
	t.Status = &api.TaskStatus{State: api.TaskStatus_ASSIGNED}
	if err := tx.Tasks().Update(&t); err != nil {
		log.Error(err)
		return nil
	}
	return &t
}

// selectNodeForTask is a naive scheduler. Will select a ready, non-drained
// node with the fewer number of tasks already running.
func (p *Planner) selectNodeForTask(t *api.Task, tx state.Tx) *api.Node {
	var target *api.Node
	targetTasks := 0

	for _, n := range p.localStore.readyNodes {
		nodeTasks := len(p.localStore.tasksByNode[n.Spec.ID])
		if target == nil || nodeTasks < targetTasks {
			target = n
			targetTasks = nodeTasks
		}
	}

	return target
}
