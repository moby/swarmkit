package drainer

import (
	"container/list"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// Drainer removes tasks which are assigned to nodes that are no longer
// responsive, or are selected for draining.
type Drainer struct {
	store       state.WatchableStore
	deleteTasks *list.List

	// stopChan signals to the state machine to stop running
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates
	doneChan chan struct{}
}

// New creates a new drainer.
func New(store state.WatchableStore) *Drainer {
	return &Drainer{
		store:       store,
		deleteTasks: list.New(),
		stopChan:    make(chan struct{}),
		doneChan:    make(chan struct{}),
	}
}

func invalidNode(n *api.Node) bool {
	return n == nil ||
		n.Status.State != api.NodeStatus_READY ||
		(n.Spec != nil && n.Spec.Availability == api.NodeAvailabilityDrain)
}

func (d *Drainer) initialPass(tx state.ReadTx) error {
	tasks, err := tx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID != "" {
			n := tx.Nodes().Get(t.NodeID)
			if invalidNode(n) && (t.Status == nil || t.Status.State != api.TaskStateDead) && t.DesiredState != api.TaskStateDead {
				d.enqueue(t)
			}
		}
	}

	return nil
}

// Run is the drainer event loop.
func (d *Drainer) Run(ctx context.Context) error {
	defer close(d.doneChan)

	updates, cancel := state.Watch(d.store.WatchQueue(),
		state.EventCreateTask{},
		state.EventUpdateTask{},
		state.EventCreateNode{},
		state.EventUpdateNode{},
		state.EventDeleteNode{},
		state.EventCommit{})
	defer cancel()

	err := d.store.View(d.initialPass)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("initial drainer pass failed")
		return err
	}

	// Remove all tasks that have an invalid node assigned
	d.tick(ctx)

	pendingChanges := 0

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.(type) {
			case state.EventCreateTask:
				pendingChanges += d.taskChanged(ctx, v.Task)
			case state.EventUpdateTask:
				pendingChanges += d.taskChanged(ctx, v.Task)
			case state.EventCreateNode:
				pendingChanges += d.nodeChanged(ctx, v.Node)
			case state.EventUpdateNode:
				pendingChanges += d.nodeChanged(ctx, v.Node)
			case state.EventDeleteNode:
				pendingChanges += d.removeTasksByNodeID(ctx, v.Node.ID)
			case state.EventCommit:
				if pendingChanges > 0 {
					d.tick(ctx)
					pendingChanges = 0
				}
			}
		case <-d.stopChan:
			return nil
		}
	}
}

// Stop causes the drainer event loop to stop running.
func (d *Drainer) Stop() {
	close(d.stopChan)
	<-d.doneChan
}

// enqueue queues a task for deletion.
func (d *Drainer) enqueue(t *api.Task) {
	d.deleteTasks.PushBack(t)
}

func (d *Drainer) taskChanged(ctx context.Context, t *api.Task) int {
	if t.NodeID == "" {
		return 0
	}

	var n *api.Node
	err := d.store.View(func(tx state.ReadTx) error {
		n = tx.Nodes().Get(t.NodeID)
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("drainer transaction failed getting tasks")
		return 0
	}
	if invalidNode(n) && (t.Status == nil || t.Status.State != api.TaskStateDead) && t.DesiredState != api.TaskStateDead {
		d.enqueue(t)
		return 1
	}
	return 0
}

func (d *Drainer) removeTasksByNodeID(ctx context.Context, nodeID string) int {
	var tasks []*api.Task
	err := d.store.View(func(tx state.ReadTx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByNodeID(nodeID))
		return err
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("drainer transaction failed removing task")
		return 0
	}

	var pendingChanges int
	for _, t := range tasks {
		d.enqueue(t)
		pendingChanges++
	}
	return pendingChanges
}

func (d *Drainer) nodeChanged(ctx context.Context, n *api.Node) int {
	if !invalidNode(n) {
		return 0
	}

	return d.removeTasksByNodeID(ctx, n.ID)
}

// tick deletes tasks that were selected for deletion.
func (d *Drainer) tick(ctx context.Context) {
	err := d.store.Update(func(tx state.Tx) error {
		var next *list.Element
		for e := d.deleteTasks.Front(); e != nil; e = next {
			next = e.Next()
			t := e.Value.(*api.Task)
			t = tx.Tasks().Get(t.ID)
			if t != nil {
				t.DesiredState = api.TaskStateDead
				err := tx.Tasks().Update(t)
				if err != nil && err != state.ErrNotExist {
					log.G(ctx).WithError(err).Errorf("failed to drain task")
				}
			}
		}
		return nil
	})

	if err != nil {
		log.G(ctx).WithError(err).Errorf("drainer tick transaction failed")
	}
	d.deleteTasks = list.New()
}
