package orchestrator

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// An Orchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for the running services.
type Orchestrator struct {
	store state.WatchableStore

	reconcileServices map[string]*api.Service
	restartTasks      map[string]struct{}

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}
}

// New creates a new orchestrator.
func New(store state.WatchableStore) *Orchestrator {
	return &Orchestrator{
		store:             store,
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
		reconcileServices: make(map[string]*api.Service),
		restartTasks:      make(map[string]struct{}),
	}
}

// Run contains the orchestrator event loop. It runs until Stop is called.
func (o *Orchestrator) Run(ctx context.Context) error {
	defer close(o.doneChan)

	// Watch changes to services and tasks
	queue := o.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Balance existing services and drain initial tasks attached to invalid
	// nodes
	err := o.store.View(func(readTx state.ReadTx) error {
		if err := o.initTasks(readTx); err != nil {
			return err
		}
		return o.initServices(readTx)
	})
	if err != nil {
		return err
	}

	o.tick(ctx)

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			o.handleTaskEvent(ctx, event)
			o.handleServiceEvent(ctx, event)
			switch event.(type) {
			case state.EventCommit:
				o.tick(ctx)
			}
		case <-o.stopChan:
			return nil
		}
	}
}

// Stop stops the orchestrator.
func (o *Orchestrator) Stop() {
	close(o.stopChan)
	<-o.doneChan
}

func (o *Orchestrator) tick(ctx context.Context) {
	// tickTasks must be called first, so we respond to task-level changes
	// before performing service reconcillation.
	o.tickTasks(ctx)
	o.tickServices(ctx)
}

func newTask(service *api.Service, instance uint64) *api.Task {
	return &api.Task{
		ID:          identity.NewID(),
		Annotations: service.Spec.Annotations, // TODO(stevvooe): Copy metadata with nice name.
		Spec:        service.Spec.Template,
		ServiceID:   service.ID,
		Instance:    instance,
		Status: &api.TaskStatus{
			State: api.TaskStateNew,
		},
		DesiredState: api.TaskStateRunning,
	}
}

// isRelatedService decides if this service is related to current orchestrator
func isRelatedService(service *api.Service) bool {
	return service != nil && service.Spec != nil && service.Spec.Mode == api.ServiceModeRunning
}
