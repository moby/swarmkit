package planner

import (
	"container/list"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
)

// A Planner assigns tasks to nodes.
type Planner struct {
	store state.WatchableStore
	queue *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// NewPlanner creates a new planner.
func NewPlanner(sDir string, store state.WatchableStore) (*Planner, error) {
	p := &Planner{
		store:    store,
		queue:    list.New(),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}

	return p, nil
}

// Run is the planner event loop.
func (p *Planner) Run() {
	defer close(p.doneChan)

	// Watch for tasks with no NodeID
	watchQueue := p.store.WatchQueue()
	unassignedTasks := state.Watch(watchQueue,
		state.EventCreateTask{Task: &api.Task{NodeID: ""},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}},
		state.EventUpdateTask{Task: &api.Task{NodeID: ""},
			Checks: []state.TaskCheckFunc{state.TaskCheckNodeID}})

	// Watch for valid nodes
	nodeChanges := state.Watch(watchQueue,
		state.EventCreateNode{Node: &api.Node{Status: api.NodeStatus{State: api.NodeStatus_READY}},
			Checks: []state.NodeCheckFunc{state.NodeCheckStatus}},
		state.EventUpdateNode{Node: &api.Node{Status: api.NodeStatus{State: api.NodeStatus_READY}},
			Checks: []state.NodeCheckFunc{state.NodeCheckStatus}})

	// Queue all unassigned tasks before watching for changes.
	tx, err := p.store.BeginRead()
	if err != nil {
		log.Errorf("Error starting transaction: %v", err)
	} else {
		tasks, err := tx.Tasks().Find(state.ByNodeID(""))
		if err != nil {
			log.Errorf("Error finding unassigned tasks: %v", err)
		} else {
			for _, t := range tasks {
				log.Infof("Queueing %#v", t)
				p.enqueue(t)
			}
		}
		tx.Close()
	}
	p.tick()

	unassignedTasksClosed := false
	nodeChangesClosed := false

	// Watch for changes.
	for {
		if unassignedTasksClosed && nodeChangesClosed {
			return
		}

		select {
		case event, ok := <-unassignedTasks:
			if !ok {
				unassignedTasksClosed = true
				continue
			}
			var task *api.Task
			switch v := event.Payload.(type) {
			case state.EventCreateTask:
				task = v.Task
			case state.EventUpdateTask:
				task = v.Task
			}
			p.enqueue(task)
			p.tick()
		case _, ok := <-nodeChanges:
			if !ok {
				nodeChangesClosed = true
				continue
			}
			p.tick()
		case <-p.stopChan:
			watchQueue.StopWatch(unassignedTasks)
			watchQueue.StopWatch(nodeChanges)
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
	p.queue.PushBack(t)
}

// tick attempts to schedule the queue.
func (p *Planner) tick() {
	var next *list.Element
	for e := p.queue.Front(); e != nil; e = next {
		next = e.Next()
		t := e.Value.(*api.Task)
		if p.scheduleTask(t) {
			p.queue.Remove(e)
		}
	}
}

// scheduleTask schedules a single task.
func (p *Planner) scheduleTask(t *api.Task) bool {
	tx, err := p.store.Begin()
	if err != nil {
		log.Errorf("Error starting transaction: %v", err)
		return false
	}
	defer tx.Close()

	node := p.selectNodeForTask(t, tx)
	if node == nil {
		log.Info("No nodes available to assign tasks to")
		return false
	}

	log.Infof("Assigning task %s to node %s", t.ID, node.Spec.ID)
	t.NodeID = node.Spec.ID
	t.Status.State = api.TaskStatus_ASSIGNED
	if err := tx.Tasks().Update(t); err != nil {
		log.Error(err)
	}
	return true
}

// selectNodeForTask is a naive scheduler. Will select a ready, non-drained
// node with the fewer number of tasks already running.
func (p *Planner) selectNodeForTask(t *api.Task, tx state.Tx) *api.Node {
	var target *api.Node
	targetTasks := 0

	nodes, err := tx.Nodes().Find(state.All)
	if err != nil {
		log.Errorf("Error listing nodes: %v", err)
		return nil
	}

	for _, n := range nodes {
		if n.Status.State == api.NodeStatus_READY /*&& !n.Drained*/ {
			tasks, err := tx.Tasks().Find(state.ByNodeID(n.Spec.ID))
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
	}

	return target
}
