package planner

import (
	"container/list"
	"errors"

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

// New creates a new planner.
func New(store state.WatchableStore) *Planner {
	return &Planner{
		store:    store,
		queue:    list.New(),
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
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
	err := p.store.View(func(tx state.ReadTx) error {
		tasks, err := tx.Tasks().Find(state.ByNodeID(""))
		if err != nil {
			log.Errorf("Error finding unassigned tasks: %v", err)
			return nil
		}
		for _, t := range tasks {
			log.Infof("Queueing %#v", t)
			p.enqueue(t)
		}
		return nil
	})
	if err != nil {
		log.Errorf("Error in transaction: %v", err)
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
	err := p.store.Update(func(tx state.Tx) error {
		node := p.selectNodeForTask(t, tx)
		if node == nil {
			err := errors.New("No nodes available to assign tasks to")
			log.Info(err)
			return err
		}

		log.Infof("Assigning task %s to node %s", t.ID, node.Spec.ID)
		t.NodeID = node.Spec.ID
		t.Status.State = api.TaskStatus_ASSIGNED
		if err := tx.Tasks().Update(t); err != nil {
			log.Error(err)
			return err
		}
		return nil
	})
	if err != nil {
		return false
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
