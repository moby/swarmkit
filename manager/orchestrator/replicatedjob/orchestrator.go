package replicatedjob

import (
	"context"
	"sync"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
)

// Orchestrator run a reconciliation loop to create and destroy tasks necessary
// for replicated jobs.
type Orchestrator struct {
	// we need the store, of course, to do updates
	store *store.MemoryStore

	// a copy of the cluster is needed, because we need it when creating tasks
	// to set the default log driver
	cluster *api.Cluster

	// reconciler holds the logic of actually operating on a service.
	reconciler reconciler

	// stopChan is a channel that is closed to signal the orchestrator to stop
	// running
	stopChan chan struct{}
	// stopOnce is used to ensure that stopChan can only be closed once, just
	// in case some freak accident causes subsequent calls to Stop.
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
	// closing doneChan should be the absolute last thing that happens in this
	// method, and so should be the absolute first thing we defer.
	defer close(o.doneChan)

	var (
		services []*api.Service
	)

	o.store.View(func(tx store.ReadTx) {
		// TODO(dperny): figure out what to do about the error return value
		// from FindServices
		services, _ = store.FindServices(tx, store.All)

		// there should only ever be 1 cluster object, but for reasons
		// forgotten by me, it needs to be retrieved in a rather roundabout way
		// from the store
		// TODO(dperny): figure out what to do with this error too
		clusters, _ := store.FindClusters(tx, store.All)
		if len(clusters) == 1 {
			o.cluster = clusters[0]
		}
	})

	// for testing purposes, if a reconciler already exists on the
	// orchestrator, we will not set it up. this allows injecting a fake
	// reconciler.
	if o.reconciler == nil {
		// the cluster might be nil, but that doesn't matter.
		o.reconciler = newReconciler(o.store, o.cluster)
	}

	for _, service := range services {
		if orchestrator.IsReplicatedJob(service) {
			// TODO(dperny): do something with the error result of
			// ReconcileService
			o.reconciler.ReconcileService(service.ID)
		}
	}

	// TODO(dperny): this will be a case in the main select loop, but for now
	// just block until stopChan is closed.
	<-o.stopChan
}

// Stop stops the Orchestrator
func (o *Orchestrator) Stop() {
	// close stopChan inside of the Once so that there can be no races
	// involving multiple attempts to close stopChan.
	o.stopOnce.Do(func() {
		close(o.stopChan)
	})
	// now, we wait for the Orchestrator to stop. this wait is unqualified; we
	// will not return until Orchestrator has stopped successfully.
	<-o.doneChan
}
