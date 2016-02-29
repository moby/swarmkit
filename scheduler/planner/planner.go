package planner

import (
	"container/list"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
)

// A Planner assigns tasks to nodes.
type Planner struct {
	store state.Store
	queue *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// NewPlanner creates a new planner.
func NewPlanner(sDir string, store state.Store) (*Planner, error) {
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
	for _, t := range p.store.TasksByNode("") {
		log.Infof("Queueing %#v", t)
		p.enqueue(t)
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
	node := p.selectNodeForTask(t)
	if node == nil {
		log.Info("No nodes available to assign tasks to")
		return false
	}

	log.Infof("Assigning task %s to node %s", t.ID, node.Spec.ID)
	t.NodeID = node.Spec.ID
	t.Status.State = api.TaskStatus_ASSIGNED
	if err := p.store.UpdateTask(t.ID, t); err != nil {
		log.Error(err)
	}
	return true
}

// selectNodeForTask is a naive scheduler. Will select a ready, non-drained
// node with the fewer number of tasks already running.
func (p *Planner) selectNodeForTask(t *api.Task) *api.Node {
	var target *api.Node
	targetTasks := 0
	for _, n := range p.store.Nodes() {
		if n.Status.State == api.NodeStatus_READY /*&& !n.Drained*/ {
			nodeTasks := len(p.store.TasksByNode(n.Spec.ID))
			if target == nil || nodeTasks < targetTasks {
				target = n
				targetTasks = nodeTasks
			}
		}
	}

	return target
}
