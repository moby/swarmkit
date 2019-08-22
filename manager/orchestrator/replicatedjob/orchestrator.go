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

	watchChan, cancel, _ := store.ViewAndWatch(o.store, func(tx store.ReadTx) error {
		// TODO(dperny): figure out what to do about the error return value
		// from FindServices
		services, _ = store.FindServices(tx, store.All)
		return nil
	})

	defer cancel()

	// for testing purposes, if a reconciler already exists on the
	// orchestrator, we will not set it up. this allows injecting a fake
	// reconciler.
	if o.reconciler == nil {
		// the cluster might be nil, but that doesn't matter.
		o.reconciler = newReconciler(o.store)
	}

	for _, service := range services {
		if orchestrator.IsReplicatedJob(service) {
			// TODO(dperny): do something with the error result of
			// ReconcileService
			o.reconciler.ReconcileService(service.ID)
		}
	}

	for {
		// first, before taking any action, see if we should stop the
		// orchestrator. if both the stop channel and the watch channel are
		// available to read, the channel that gets read is picked at random,
		// but we always want to stop if it's possible.
		select {
		case <-o.stopChan:
			return
		default:
		}

		select {
		case event := <-watchChan:
			var service *api.Service

			switch ev := event.(type) {
			case api.EventCreateService:
				service = ev.Service
			case api.EventUpdateService:
				service = ev.Service
			}

			if service != nil {
				o.reconciler.ReconcileService(service.ID)
			}
		case <-o.stopChan:
			// we also need to check for stop in here, in case there are no
			// updates to cause the loop to turn over.
			return
		}
	}
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
