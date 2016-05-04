package orchestrator

import (
	"time"

	"github.com/docker/swarm-v2/api"
	tspb "github.com/docker/swarm-v2/api/timestamp"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
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

	updater *UpdateSupervisor
}

// New creates a new orchestrator.
func New(store state.WatchableStore) *Orchestrator {
	return &Orchestrator{
		store:             store,
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
		reconcileServices: make(map[string]*api.Service),
		restartTasks:      make(map[string]struct{}),
		updater:           NewUpdateSupervisor(store),
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
	var err error
	o.store.View(func(readTx state.ReadTx) {
		if err = o.initTasks(readTx); err != nil {
			return
		}
		err = o.initServices(readTx)
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
	o.updater.CancelAll()
}

func (o *Orchestrator) tick(ctx context.Context) {
	// tickTasks must be called first, so we respond to task-level changes
	// before performing service reconcillation.
	o.tickTasks(ctx)
	o.tickServices(ctx)
}

func newTask(service *api.Service, instance uint64) *api.Task {
	t := time.Now()
	seconds := t.Unix()
	nanos := int32(t.Sub(time.Unix(seconds, 0)))
	ts := &tspb.Timestamp{
		Seconds: seconds,
		Nanos:   nanos,
	}

	return &api.Task{
		ID:          identity.NewID(),
		Annotations: service.Spec.Annotations, // TODO(stevvooe): Copy metadata with nice name.
		Spec:        *service.Spec.Template,
		ServiceID:   service.ID,
		Instance:    instance,
		Status: api.TaskStatus{
			State:     api.TaskStateNew,
			Timestamp: ts,
		},
		DesiredState: api.TaskStateRunning,
	}
}

// isRelatedService decides if this service is related to current orchestrator
func isRelatedService(service *api.Service) bool {
	return service != nil && service.Spec.Mode == api.ServiceModeRunning
}

func deleteServiceTasks(ctx context.Context, store state.Store, service *api.Service) {
	var (
		tasks []*api.Task
		err   error
	)
	store.View(func(tx state.ReadTx) {
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to list tasks")
		return
	}

	_, err = store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			err := batch.Update(func(tx state.Tx) error {
				if err := tx.Tasks().Delete(t.ID); err != nil {
					log.G(ctx).WithError(err).Errorf("failed to delete task")
				}
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("task search transaction failed")
	}
}
