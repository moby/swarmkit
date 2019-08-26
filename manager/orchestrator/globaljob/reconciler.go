package globaljob

import (
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/constraint"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
)

type reconciler interface {
	ReconcileService(id string) error
}

type reconcilerObj struct {
	store *store.MemoryStore
}

func newReconciler(store *store.MemoryStore) reconciler {
	return &reconcilerObj{
		store: store,
	}
}

func (r *reconcilerObj) ReconcileService(id string) error {
	var (
		service *api.Service
		cluster *api.Cluster
		tasks   []*api.Task
		nodes   []*api.Node
	)

	// we need to first get the latest iteration of the service, its tasks, and
	// the nodes in the cluster.
	r.store.View(func(tx store.ReadTx) {
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

		clusters, _ := store.FindClusters(tx, store.All)
		if len(clusters) == 1 {
			cluster = clusters[0]
		} else if len(clusters) > 1 {
			panic("there should never be more than one cluster object")
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

	return r.store.Batch(func(batch *store.Batch) error {
		for _, node := range candidateNodes {
			// check if there is a task for this node ID. If not, then we need
			// to create one.
			if _, ok := nodeToTask[node]; !ok {
				if err := batch.Update(func(tx store.Tx) error {
					// if the node does not already have a running or completed
					// task, create a task for this node.
					task := orchestrator.NewTask(cluster, service, 0, node)
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
