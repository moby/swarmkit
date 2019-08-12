package globaljob

import (
	"context"
	"fmt"
	"sync"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/constraint"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
)

type Orchestrator struct {
	store *store.MemoryStore

	// startOnce is a function that stops the orchestrator from being started
	// multiple times
	startOnce sync.Once

	// stopChan is a channel that is closed to signal the orchestrator to stop
	// running
	stopChan chan struct{}
	// stopOnce is used to ensure that stopChan can only be closed once, just
	// in case some freak accident causes multiple calls to Stop
	stopOnce sync.Once
	// doneChan is closed when the orchestrator actually stops running
	doneChan chan struct{}
}

func NewOrchestrator(store *store.MemoryStore) *Orchestrator {
	return &Orchestrator{
		store:    store,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Run runs the Orchestrator reconciliation loop. It takes a context as an
// argument, but canceling this context will not stop the routine; this context
// is only for passing in logging information. Call Stop to stop the
// Orchestrator
func (o *Orchestrator) Run(ctx context.Context) {
	// startOnce and only once.
	o.startOnce.Do(func() { o.run(ctx) })
}

// run provides the actual meat of the the run operation. The call to run is
// made inside of Run, and is enclosed in a sync.Once to stop this from being
// called multiple times
func (o *Orchestrator) run(ctx context.Context) {
	// the first thing we do is defer closing doneChan, as this should be the
	// last thing we do in this function, after all other defers
	defer close(o.doneChan)

	// for now, just to get this whole code-writing thing going, we'll just
	// block until Stop is called.
	<-o.stopChan
}

// reconcileService determines whether a service has enough tasks, and creates
// more if needed.
func (o *Orchestrator) reconcileService(id string) error {
	var (
		service *api.Service
		tasks   []*api.Task
		nodes   []*api.Node
	)

	// we need to first get the latest iteration of the service, its tasks, and
	// the nodes in the cluster.
	o.store.View(func(tx store.ReadTx) {
		service = store.GetService(tx, id)
		if service == nil {
			return
		}

		// getting tasks with FindTasks should only return an error if we've
		// made a mistake coding; there's no user-input or even reasonable
		// system state that can cause it. If it returns an error, we'll just
		// panic and crash.
		var err error
		tasks, err = store.FindTasks(tx, store.ByServiceID(id))
		if err != nil {
			panic(fmt.Sprintf("error getting tasks: %v", err))
		}

		// same as with FindTasks
		nodes, err = store.FindNodes(tx, store.All)
		if err != nil {
			panic(fmt.Sprintf("error getting nodes: %v", err))
		}
	})

	// the service may be nil if the service has been deleted before we entered
	// the View.
	if service == nil {
		return nil
	}

	if service.JobStatus == nil {
		service.JobStatus = &api.JobStatus{}
	}

	// we need to compute the constraints on the service so we know which nodes
	// to schedule it on
	var constraints []constraint.Constraint
	if service.Spec.Task.Placement != nil && len(service.Spec.Task.Placement.Constraints) != 0 {
		// constraint.Parse does return an error, but we don't need to check
		// it, because it was already checked when the service was created or
		// updated.
		constraints, _ = constraint.Parse(service.Spec.Task.Placement.Constraints)
	}

	// here's the tricky part: we only need to schedule the global job on nodes
	// that existed when the job was created. of course, because this is a
	// distributed system, time is meaningless and clock skew is a given;
	// however, the consequences for getting this _wrong_ aren't
	// earth-shattering, and the clock skew can't be _that_ bad because the PKI
	// wouldn't work if it was. so this is just a best effort endeavor. we'll
	// schedule to any node that says its creation time is before the
	// LastExecution time.
	//
	// TODO(dperny): this only covers the case of nodes that were added after a
	// global job is created. if nodes are paused or drained when a global job
	// is started, then when they become un-paused or un-drained, the job will
	// execute on them. i'm unsure if at this point it's better to accept and
	// document this behavior, or spend rather a lot of time and energy coming
	// up with a fix.
	lastExecution, err := gogotypes.TimestampFromProto(service.JobStatus.LastExecution)
	if err != nil {
		// TODO(dperny): validate that lastExecution is set on service creation
		// or update.
		lastExecution, err = gogotypes.TimestampFromProto(service.Meta.CreatedAt)
		if err != nil {
			panic(fmt.Sprintf("service CreateAt time could not be parsed: %v", err))
		}
	}

	var candidateNodes []string
	for _, node := range nodes {
		// instead of having a big ugly multi-line boolean expression in the
		// if-statement, we'll have several if-statements, and bail out of
		// this loop iteration with continue if the node is not acceptable
		if !constraint.NodeMatches(constraints, node) {
			continue
		}
		if node.Spec.Availability != api.NodeAvailabilityActive {
			continue
		}
		if node.Status.State != api.NodeStatus_READY {
			continue
		}
		nodeCreationTime, err := gogotypes.TimestampFromProto(node.Meta.CreatedAt)
		if err != nil || !nodeCreationTime.Before(lastExecution) {
			continue
		}
		// you can append to a nil slice and get a non-nil slice, which is
		// pretty slick.
		candidateNodes = append(candidateNodes, node.ID)
	}

	// now, we have a list of all nodes that match constraints. it's time to
	// match running tasks to the nodes. we need to identify all nodes that
	// need new tasks, which is any node that doesn't have a task of this job
	// iteration. trade some space for some time by building a node ID to task
	// ID mapping, so that we're just doing 2x linear operation, instead of a
	// quadratic operation.
	nodeToTask := map[string]string{}
	for _, task := range tasks {
		// only match tasks belonging to this job iteration, and running or
		// completed, which are not desired to be shut down
		if task.JobIteration != nil &&
			task.JobIteration.Index == service.JobStatus.JobIteration.Index &&
			task.Status.State <= api.TaskStateCompleted &&
			task.DesiredState <= api.TaskStateCompleted {
			nodeToTask[task.NodeID] = task.ID
		}
	}

	return o.store.Batch(func(batch *store.Batch) error {
		for _, node := range candidateNodes {
			// check if there is a task for this node ID. If not, then we need
			// to create one.
			if _, ok := nodeToTask[node]; !ok {
				if err := batch.Update(func(tx store.Tx) error {
					// if the node does not already have a running or completed
					// task, create a task for this node.
					// TODO(dperny): pass non-nil orchestrator
					task := orchestrator.NewTask(nil, service, 0, node)
					task.JobIteration = &service.JobStatus.JobIteration
					task.DesiredState = api.TaskStateCompleted
					return store.CreateTask(tx, task)
				}); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (o *Orchestrator) Stop() {
	o.stopOnce.Do(func() {
		close(o.stopChan)
	})

	// wait for doneChan to close. Unconditional, blocking wait, because
	// obviously I have that much faith in my code.
	<-o.doneChan
}
