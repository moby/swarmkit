package orchestrator

import (
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/docker/swarm-v2/protobuf/ptypes"
	"golang.org/x/net/context"
)

// An ReplicatedOrchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for the replicated services.
type ReplicatedOrchestrator struct {
	store *store.MemoryStore

	reconcileServices map[string]*api.Service
	restartTasks      map[string]struct{}

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}

	updater  *UpdateSupervisor
	restarts *RestartSupervisor
}

// New creates a new ReplicatedOrchestrator.
func New(store *store.MemoryStore) *ReplicatedOrchestrator {
	restartSupervisor := NewRestartSupervisor(store)
	updater := NewUpdateSupervisor(store, restartSupervisor)
	return &ReplicatedOrchestrator{
		store:             store,
		stopChan:          make(chan struct{}),
		doneChan:          make(chan struct{}),
		reconcileServices: make(map[string]*api.Service),
		restartTasks:      make(map[string]struct{}),
		updater:           updater,
		restarts:          restartSupervisor,
	}
}

// Run contains the orchestrator event loop. It runs until Stop is called.
func (r *ReplicatedOrchestrator) Run(ctx context.Context) error {
	defer close(r.doneChan)

	// Watch changes to services and tasks
	queue := r.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Balance existing services and drain initial tasks attached to invalid
	// nodes
	var err error
	r.store.View(func(readTx store.ReadTx) {
		if err = r.initTasks(ctx, readTx); err != nil {
			return
		}
		err = r.initServices(readTx)
	})
	if err != nil {
		return err
	}

	r.tick(ctx)

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			r.handleTaskEvent(ctx, event)
			r.handleServiceEvent(ctx, event)
			switch event.(type) {
			case state.EventCommit:
				r.tick(ctx)
			}
		case <-r.stopChan:
			return nil
		}
	}
}

// Stop stops the orchestrator.
func (r *ReplicatedOrchestrator) Stop() {
	close(r.stopChan)
	<-r.doneChan
	r.updater.CancelAll()
	r.restarts.CancelAll()
}

func (r *ReplicatedOrchestrator) tick(ctx context.Context) {
	// tickTasks must be called first, so we respond to task-level changes
	// before performing service reconcillation.
	r.tickTasks(ctx)
	r.tickServices(ctx)
}

func newTask(service *api.Service, instance uint64) *api.Task {
	containerSpec := api.ContainerSpec{}
	if service.Spec.GetContainer() != nil {
		containerSpec = *service.Spec.GetContainer().Copy()

		// NOTE(stevvooe): For now, we don't override the container naming and
		// labeling scheme in the agent. If we decide to do this in the future,
		// they should be overridden here.
	}

	return &api.Task{
		ID:                 identity.NewID(),
		ServiceAnnotations: service.Spec.Annotations,
		Runtime: &api.Task_Container{
			Container: &api.Container{
				Spec: containerSpec,
			},
		},
		ServiceID: service.ID,
		Instance:  instance,
		Status: api.TaskStatus{
			State:     api.TaskStateNew,
			Timestamp: ptypes.MustTimestampProto(time.Now()),
			Message:   "created",
		},
		DesiredState: api.TaskStateRunning,
	}
}

// isReplicatedService checks if a service is a replicated service
func isReplicatedService(service *api.Service) bool {
	// service nil validation is required as there are scenarios
	// where service is removed from store
	if service == nil {
		return false
	}
	_, ok := service.Spec.GetMode().(*api.ServiceSpec_Replicated)
	return ok
}

func deleteServiceTasks(ctx context.Context, s *store.MemoryStore, service *api.Service) {
	var (
		tasks []*api.Task
		err   error
	)
	s.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to list tasks")
		return
	}

	_, err = s.Batch(func(batch *store.Batch) error {
		for _, t := range tasks {
			err := batch.Update(func(tx store.Tx) error {
				if err := store.DeleteTask(tx, t.ID); err != nil {
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

func restartCondition(service *api.Service) api.RestartPolicy_RestartCondition {
	restartCondition := api.RestartOnAny
	if service.Spec.Restart != nil {
		restartCondition = service.Spec.Restart.Condition
	}
	return restartCondition
}
