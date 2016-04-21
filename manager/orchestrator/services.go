package orchestrator

import (
	"reflect"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// This file provices service-level orchestration. It observes changes to
// services and creates and destroys tasks as necessary to match the service
// specifications. This is different from task-level orchestration, which
// responds to changes in individual tasks (or nodes which run them).

func (o *Orchestrator) initServices(readTx state.ReadTx) error {
	runningServices, err := readTx.Services().Find(state.ByServiceMode(api.ServiceModeRunning))
	if err != nil {
		return err
	}
	for _, s := range runningServices {
		o.reconcileServices[s.ID] = s
	}
	return nil
}

func (o *Orchestrator) handleServiceEvent(ctx context.Context, event events.Event) {
	switch v := event.(type) {
	case state.EventDeleteService:
		if !isRelatedService(v.Service) {
			return
		}
		o.handleDeleteService(ctx, v.Service)
	case state.EventCreateService:
		if !isRelatedService(v.Service) {
			return
		}
		o.reconcileServices[v.Service.ID] = v.Service
	case state.EventUpdateService:
		if !isRelatedService(v.Service) {
			return
		}
		o.reconcileServices[v.Service.ID] = v.Service
	}
}

func (o *Orchestrator) tickServices(ctx context.Context) {
	if len(o.reconcileServices) > 0 {
		for _, s := range o.reconcileServices {
			o.reconcile(ctx, s)
		}
		o.reconcileServices = make(map[string]*api.Service)
	}
}

func (o *Orchestrator) handleDeleteService(ctx context.Context, service *api.Service) {
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
		log.G(ctx).WithError(err).Errorf("handleDeleteService transaction failed")
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
		log.G(ctx).WithError(err).Errorf("handleDeleteService transaction failed")
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

func restartCondition(service *api.Service) api.RestartPolicy_RestartCondition {
	restartCondition := api.RestartAlways
	if service.Spec.Restart != nil {
		restartCondition = service.Spec.Restart.Condition
	}
	return restartCondition
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

	if service.Spec == nil {
		return
	}

	runningTasks := make([]*api.Task, 0, len(tasks))
	for _, t := range tasks {
		if t.DesiredState == api.TaskStateRunning {
			runningTasks = append(runningTasks, t)
		}
	}
	numTasks := len(runningTasks)

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
			log.G(ctx).Debugf("Service %s was scaled down from %d to %d instances", service.ID, numTasks, specifiedInstances)
			o.updateTasks(ctx, batch, service, runningTasks[:specifiedInstances])
			o.removeTasks(ctx, batch, service, runningTasks[specifiedInstances:])

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
	for i := 0; i < count; i++ {
		err := batch.Update(func(tx state.Tx) error {
			return tx.Tasks().Create(newTask(service))
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
