package allocator

import (
	"github.com/pkg/errors"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

// Allocator controls how the allocation stage in the manager is handled.
type Allocator struct {
	// The manager store.
	store *store.MemoryStore

	// context for the network allocator that will be needed by
	// network allocator.
	netCtx *networkContext

	// stopChan signals to the allocator to stop running.
	stopChan chan struct{}
	// doneChan is closed when the allocator is finished running.
	doneChan chan struct{}

	// pluginGetter provides access to docker's plugin inventory.
	pluginGetter plugingetter.PluginGetter
}

// New returns a new instance of Allocator for use during allocation
// stage of the manager.
func New(store *store.MemoryStore, pg plugingetter.PluginGetter) (*Allocator, error) {
	a := &Allocator{
		store:        store,
		stopChan:     make(chan struct{}),
		doneChan:     make(chan struct{}),
		pluginGetter: pg,
	}

	return a, nil
}

// Run starts all allocator go-routines and waits for Stop to be called.
func (a *Allocator) Run(ctx context.Context) error {
	// Setup cancel context for all goroutines to use.
	ctx, cancel := context.WithCancel(ctx)

	defer func() {
		cancel()
		close(a.doneChan)
	}()
	watch, watchCancel := state.Watch(a.store.WatchQueue(),
		api.EventCreateNetwork{},
		api.EventDeleteNetwork{},
		api.EventCreateService{},
		api.EventUpdateService{},
		api.EventDeleteService{},
		api.EventCreateTask{},
		api.EventUpdateTask{},
		api.EventDeleteTask{},
		api.EventCreateNode{},
		api.EventUpdateNode{},
		api.EventDeleteNode{},
		state.EventCommit{},
	)
	defer watchCancel()

	if err := a.doNetworkInit(ctx); err != nil {
		return err
	}

	for {
		select {
		case ev, ok := <-watch:
			if !ok {
				// the channel should not be closed unless we stop it, and if
				// it does close that's an error
				return errors.New("allocator event watch closed unexpectedly")
			}

			a.doNetworkAlloc(ctx, ev)
		case <-ctx.Done():
			return ctx.Err()
		case <-a.stopChan:
			return nil
		}
	}
}

// Stop stops the allocator
func (a *Allocator) Stop() {
	close(a.stopChan)
	// Wait for all allocator goroutines to truly exit
	<-a.doneChan
}
