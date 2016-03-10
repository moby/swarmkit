package planner

import (
	"container/list"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
)

// A Planner assigns tasks to nodes.
type Planner struct {
	store           state.WatchableStore
	unassignedTasks *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// New creates a new planner.
func New(store state.WatchableStore) *Planner {
	return &Planner{
		store:           store,
		unassignedTasks: list.New(),
		stopChan:        make(chan struct{}),
		doneChan:        make(chan struct{}),
	}
}

func (p *Planner) setupTasksList(tx state.ReadTx) error {
	tasks, err := tx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID == "" {
			log.Infof("Queueing %#v", t)
			p.enqueue(t)
		}
	}

	return nil
}

// Run is the planner event loop.
func (p *Planner) Run() error {
	defer close(p.doneChan)

	updates := p.store.WatchQueue().Watch()
	defer p.store.WatchQueue().StopWatch(updates)

	err := p.store.View(p.setupTasksList)
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
		case event := <-updates:
			switch v := event.Payload.(type) {
			case state.EventCreateTask:
				pendingChanges += p.createTask(v.Task)
			case state.EventUpdateTask:
				pendingChanges += p.createTask(v.Task)
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
					p.tick()
					pendingChanges = 0
				}
			}
		case <-p.stopChan:
			return nil
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
	p.unassignedTasks.PushBack(t)
}

func (p *Planner) createTask(t *api.Task) int {
	if t.NodeID == "" {
		// unassigned task
		p.enqueue(t)
		return 1
	}
	return 0
}

// tick attempts to schedule the queue.
func (p *Planner) tick() {
	nextBatch := list.New()

	// TODO(aaronl): Ideally, we would make scheduling decisions outside
	// of an Update callback, since Update blocks other writes to the
	// store. The current approach of making the decisions inside Update
	// is done to keep the store simple. Eventually, we may want to break
	// this up into a View where the decisions are made, and an Update that
	// applies them. This will require keeping local state to keep track of
	// allocations as they are made, since the store itself can't be
	// changed through View.
	err := p.store.Update(func(tx state.Tx) error {
		nodes, err := tx.Nodes().Find(state.All)
		if err != nil {
			return err
		}

		var next *list.Element
		for e := p.unassignedTasks.Front(); e != nil; e = next {
			next = e.Next()
			t := e.Value.(*api.Task)
			if newT := p.scheduleTask(tx, nodes, *t); newT == nil {
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
		p.unassignedTasks = nextBatch
	}
}

// scheduleTask schedules a single task.
func (p *Planner) scheduleTask(tx state.Tx, nodes []*api.Node, t api.Task) *api.Task {
	node := p.selectNodeForTask(tx, nodes, &t)
	if node == nil {
		log.Info("No nodes available to assign tasks to")
		return nil
	}

	log.Infof("Assigning task %s to node %s", t.ID, node.ID)
	t.NodeID = node.ID
	t.Status = &api.TaskStatus{State: api.TaskStatus_ASSIGNED}
	if err := tx.Tasks().Update(&t); err != nil {
		log.Error(err)
		return nil
	}
	return &t
}

// selectNodeForTask is a naive scheduler. Will select a ready, non-drained
// node with the fewer number of tasks already running.
func (p *Planner) selectNodeForTask(tx state.Tx, nodes []*api.Node, t *api.Task) *api.Node {
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
