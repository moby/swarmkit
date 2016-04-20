package orchestrator

import (
	"reflect"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// An Orchestrator runs a reconciliation loop to create and destroy
// tasks as necessary for the running services.
type Orchestrator struct {
	store state.WatchableStore

	// stopChan signals to the state machine to stop running.
	stopChan chan struct{}
	// doneChan is closed when the state machine terminates.
	doneChan chan struct{}
}

// New creates a new orchestrator.
func New(store state.WatchableStore) *Orchestrator {
	return &Orchestrator{
		store:    store,
		stopChan: make(chan struct{}),
		doneChan: make(chan struct{}),
	}
}

// Run contains the orchestrator event loop. It runs until Stop is called.
func (o *Orchestrator) Run(ctx context.Context) error {
	defer close(o.doneChan)

	// Watch changes to services and tasks
	queue := o.store.WatchQueue()
	watcher, cancel := queue.Watch()
	defer cancel()

	// Balance existing services
	var existingServices []*api.Service
	err := o.store.View(func(readTx state.ReadTx) error {
		var err error
		existingServices, err = readTx.Services().Find(state.All)
		return err
	})
	if err != nil {
		return err
	}

	for _, j := range existingServices {
		o.reconcile(ctx, j)
	}

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			switch v := event.(type) {
			case state.EventDeleteService:
				o.deleteService(ctx, v.Service)
			case state.EventCreateService:
				o.reconcile(ctx, v.Service)
			case state.EventUpdateService:
				o.reconcile(ctx, v.Service)
			case state.EventDeleteTask:
				o.deleteTask(ctx, v.Task)
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

func (o *Orchestrator) deleteService(ctx context.Context, service *api.Service) {
	log.G(ctx).Debugf("Service %s was deleted", service.ID)
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.G(ctx).WithError(err).Errorf("failed finding tasks for service")
			return err
		}
		for _, t := range tasks {
			// TODO(aluzzardi): Find a better way to deal with errors.
			// If `Delete` fails, it probably means the task was already deleted which is fine.
			_ = tx.Tasks().Delete(t.ID)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleteService transaction failed")
	}
}

func (o *Orchestrator) deleteTask(ctx context.Context, task *api.Task) {
	if task.ServiceID != "" {
		var service *api.Service
		err := o.store.View(func(tx state.ReadTx) error {
			service = tx.Services().Get(task.ServiceID)
			return nil
		})
		if err != nil {
			log.G(ctx).WithError(err).Errorf("deleteTask transaction failed")
		}
		if service != nil {
			o.reconcile(ctx, service)
		}
	}
}

func (o *Orchestrator) reconcile(ctx context.Context, service *api.Service) {
	err := o.store.Update(func(tx state.Tx) error {
		tasks, err := tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.G(ctx).WithError(err).Errorf("reconcile failed finding tasks")
			return nil
		}
		numTasks := int64(len(tasks))
		specifiedInstances := service.Spec.Instances

		switch {
		case specifiedInstances > numTasks:
			log.G(ctx).Debugf("Service %s was scaled up from %d to %d instances", service.ID, numTasks, specifiedInstances)
			// Update all current tasks then add missing tasks
			o.updateTasks(ctx, tx, service, tasks)
			o.addTasks(ctx, tx, service, specifiedInstances-numTasks)

		case specifiedInstances < numTasks:
			// TODO(aaronl): Scaling down needs to involve the
			// planner. The orchestrator should not be deleting
			// tasks directly.
			log.G(ctx).Debugf("Service %s was scaled down from %d to %d instances", service.ID, numTasks, specifiedInstances)
			// Update up to N tasks then remove the extra
			o.updateTasks(ctx, tx, service, tasks[0:specifiedInstances])
			o.removeTasks(ctx, tx, service, tasks[specifiedInstances:numTasks])

		case specifiedInstances == numTasks:
			// Simple update, no scaling - update all tasks.
			o.updateTasks(ctx, tx, service, tasks)
		}

		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("reconcile transaction failed")
	}
}

func (o *Orchestrator) updateTasks(ctx context.Context, tx state.Tx, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		if reflect.DeepEqual(service.Spec.Template, t.Spec) {
			continue
		}
		o.addTasks(ctx, tx, service, 1)
		if err := tx.Tasks().Delete(t.ID); err != nil {
			log.G(ctx).Errorf("Failed to remove %s: %v", t.ID, err)
		}
	}
}

func (o *Orchestrator) addTasks(ctx context.Context, tx state.Tx, service *api.Service, count int64) {
	spec := *service.Spec.Template
	meta := service.Spec.Annotations // TODO(stevvooe): Copy metadata with nice name.

	for i := int64(0); i < count; i++ {
		task := &api.Task{
			ID:          identity.NewID(),
			Annotations: meta,
			Spec:        &spec,
			ServiceID:   service.ID,
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
		}
		if err := tx.Tasks().Create(task); err != nil {
			log.G(ctx).Errorf("Failed to create task: %v", err)
		}
	}
}

func (o *Orchestrator) removeTasks(ctx context.Context, tx state.Tx, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		// TODO(aluzzardi): Find a better way to deal with errors.
		// If `Delete` fails, it probably means the task was already deleted which is fine.
		if err := tx.Tasks().Delete(t.ID); err != nil {
			log.G(ctx).WithError(err).Errorf("removing task %s failed", t.ID)
		}
	}
}
