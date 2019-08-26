package globaljob

import (
	"context"
	"sync"

	"github.com/docker/swarmkit/manager/state/store"
)

type Orchestrator struct {
	store *store.MemoryStore

	// reconciler is used to decouple reconciliation logic from event loop
	// logic, and allows injecting a fake reconciler.
	reconciler reconciler

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

// NewOrchestrator creates a new global jobs orchestrator.
func NewOrchestrator(store *store.MemoryStore) *Orchestrator {
	reconciler := newReconciler(store)
	return &Orchestrator{
		store:      store,
		reconciler: reconciler,
		stopChan:   make(chan struct{}),
		doneChan:   make(chan struct{}),
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

func (o *Orchestrator) Stop() {
	o.stopOnce.Do(func() {
		close(o.stopChan)
	})

	// wait for doneChan to close. Unconditional, blocking wait, because
	// obviously I have that much faith in my code.
	<-o.doneChan
}
