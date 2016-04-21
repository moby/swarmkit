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
		if !isRelatedService(j) {
			continue
		}
		o.reconcile(ctx, j)
	}

	servicesToReconcile := make(map[string]*api.Service)

	for {
		select {
		case event := <-watcher:
			// TODO(stevvooe): Use ctx to limit running time of operation.
			switch v := event.(type) {
			case state.EventDeleteService:
				if !isRelatedService(v.Service) {
					continue
				}
				o.deleteService(ctx, v.Service)
			case state.EventCreateService:
				if !isRelatedService(v.Service) {
					continue
				}
				servicesToReconcile[v.Service.ID] = v.Service
			case state.EventUpdateService:
				if !isRelatedService(v.Service) {
					continue
				}
				servicesToReconcile[v.Service.ID] = v.Service
			case state.EventDeleteTask:
				service := o.resolveService(ctx, v.Task)
				if !isRelatedService(service) {
					continue
				}
				servicesToReconcile[service.ID] = service
			case state.EventUpdateTask:
				service := o.resolveService(ctx, v.Task)
				if !isRelatedService(service) {
					continue
				}
				servicesToReconcile[service.ID] = service
			case state.EventCommit:
				if len(servicesToReconcile) > 0 {
					for _, s := range servicesToReconcile {
						o.reconcile(ctx, s)
					}
					servicesToReconcile = make(map[string]*api.Service)
				}
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

	var tasks []*api.Task
	err := o.store.View(func(tx state.ReadTx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.G(ctx).WithError(err).Errorf("failed finding tasks for service")
			return err
		}

		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleteService transaction failed")
		return
	}

	_, err = o.store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			// TODO(aaronl): optimistic update?
			err := batch.Update(func(tx state.Tx) error {
				t = tx.Tasks().Get(t.ID)
				if t != nil {
					t.DesiredState = api.TaskStateDead
					return tx.Tasks().Update(t)
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
		log.G(ctx).WithError(err).Errorf("deleteService transaction failed")
	}
}

func (o *Orchestrator) resolveService(ctx context.Context, task *api.Task) *api.Service {
	if task.ServiceID == "" {
		return nil
	}
	var service *api.Service
	err := o.store.View(func(tx state.ReadTx) error {
		service = tx.Services().Get(task.ServiceID)
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleteTask transaction failed")
	}
	return service
}

// taskRunning checks whether a task is either actively running, or in the
// process of starting up.
func taskRunning(t *api.Task) bool {
	return t.DesiredState == api.TaskStateRunning && t.Status != nil && t.Status.State <= api.TaskStateRunning
}

// taskSuccessful checks whether a task completed successfully.
func taskSuccessful(t *api.Task) bool {
	return t.Status != nil && t.Status.TerminalState == api.TaskStateCompleted
}

func (o *Orchestrator) reconcile(ctx context.Context, service *api.Service) {
	var tasks []*api.Task
	err := o.store.View(func(tx state.ReadTx) error {
		var err error
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
		if err != nil {
			log.G(ctx).WithError(err).Errorf("reconcile failed finding tasks")
		}
		return err
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("reconcile transaction failed")
		return
	}

	restartCondition := api.RestartAlways
	if service.Spec.Restart != nil {
		restartCondition = service.Spec.Restart.Condition
	}

	var numTasks int
	runningTasks := make([]*api.Task, 0, len(tasks))
	for _, t := range tasks {
		if taskRunning(t) {
			runningTasks = append(runningTasks, t)
		}
	}

	switch restartCondition {
	case api.RestartAlways:
		// For "always restart", we seek to balance
		// running_tasks == instances
		numTasks = len(runningTasks)
	case api.RestartOnFailure:
		// For "restart on failure", we seek to balance
		// running_tasks + successful_tasks == instances
		// TODO(aaronl): This may need to become more complex
		// when we garbage collect tasks.
		for _, t := range tasks {
			if taskRunning(t) || taskSuccessful(t) {
				numTasks++
			}
		}
	case api.RestartNever:
		// For "restart on failure", we seek to balance
		// running_tasks + failed_tasks + successful_tasks == instances
		// TODO(aaronl): This may need to become more complex
		// when we garbage collect tasks.
		numTasks = len(tasks)
	}
	specifiedInstances := int(service.Spec.Instances)

	// TODO(aaronl): Add support for restart delays.

	_, err = o.store.Batch(func(batch state.Batch) error {
		switch {
		case specifiedInstances > numTasks:
			log.G(ctx).Debugf("Service %s was scaled up from %d to %d instances", service.ID, numTasks, specifiedInstances)
			// Update all current tasks then add missing tasks
			o.updateTasks(ctx, batch, service, runningTasks)
			o.addTasks(ctx, batch, service, specifiedInstances-numTasks)

		case specifiedInstances < numTasks:
			// Update up to N tasks then remove the extra
			if specifiedInstances > len(runningTasks) {
				o.updateTasks(ctx, batch, service, runningTasks)
			} else {
				log.G(ctx).Debugf("Service %s was scaled down from %d to %d instances", service.ID, numTasks, specifiedInstances)
				o.updateTasks(ctx, batch, service, runningTasks[:specifiedInstances])
				o.removeTasks(ctx, batch, service, runningTasks[specifiedInstances:])
			}

		case specifiedInstances == numTasks:
			// Simple update, no scaling - update all tasks.
			o.updateTasks(ctx, batch, service, runningTasks)
		}
		return nil
	})

	if err != nil {
		log.G(ctx).WithError(err).Errorf("reconcile batch failed")
	}
}

func (o *Orchestrator) updateTasks(ctx context.Context, batch state.Batch, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		if reflect.DeepEqual(service.Spec.Template, t.Spec) {
			continue
		}
		o.addTasks(ctx, batch, service, 1)
		err := batch.Update(func(tx state.Tx) error {
			// TODO(aaronl): optimistic update?
			t = tx.Tasks().Get(t.ID)
			if t != nil {
				t.DesiredState = api.TaskStateDead
				return tx.Tasks().Update(t)
			}
			return nil
		})
		if err != nil {
			log.G(ctx).Errorf("Failed to remove %s: %v", t.ID, err)
		}
	}
}

func (o *Orchestrator) addTasks(ctx context.Context, batch state.Batch, service *api.Service, count int) {
	spec := *service.Spec.Template
	meta := service.Spec.Annotations // TODO(stevvooe): Copy metadata with nice name.

	for i := 0; i < count; i++ {
		task := &api.Task{
			ID:          identity.NewID(),
			Annotations: meta,
			Spec:        &spec,
			ServiceID:   service.ID,
			Status: &api.TaskStatus{
				State: api.TaskStateNew,
			},
			DesiredState: api.TaskStateRunning,
		}
		err := batch.Update(func(tx state.Tx) error {
			return tx.Tasks().Create(task)
		})
		if err != nil {
			log.G(ctx).Errorf("Failed to create task: %v", err)
		}
	}
}

func (o *Orchestrator) removeTasks(ctx context.Context, batch state.Batch, service *api.Service, tasks []*api.Task) {
	for _, t := range tasks {
		err := batch.Update(func(tx state.Tx) error {
			// TODO(aaronl): optimistic update?
			t = tx.Tasks().Get(t.ID)
			if t != nil {
				t.DesiredState = api.TaskStateDead
				return tx.Tasks().Update(t)
			}
			return nil
		})
		if err != nil {
			log.G(ctx).WithError(err).Errorf("removing task %s failed", t.ID)
		}
	}
}

// isRelatedService decides if this service is related to current orchestrator
func isRelatedService(service *api.Service) bool {
	return service != nil && service.Spec != nil && service.Spec.Mode == api.ServiceModeRunning
}
