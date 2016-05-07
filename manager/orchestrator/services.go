package orchestrator

import (
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
		deleteServiceTasks(ctx, o.store, v.Service)
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

func (o *Orchestrator) resolveService(ctx context.Context, task *api.Task) *api.Service {
	if task.ServiceID == "" {
		return nil
	}
	var service *api.Service
	o.store.View(func(tx state.ReadTx) {
		service = tx.Services().Get(task.ServiceID)
	})
	return service
}

func (o *Orchestrator) reconcile(ctx context.Context, service *api.Service) {
	var (
		tasks []*api.Task
		err   error
	)
	o.store.View(func(tx state.ReadTx) {
		tasks, err = tx.Tasks().Find(state.ByServiceID(service.ID))
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("reconcile failed finding tasks")
		return
	}

	runningTasks := make([]*api.Task, 0, len(tasks))
	runningInstances := make(map[uint64]struct{}) // this could be a bitfield...
	for _, t := range tasks {
		// Technically the check below could just be
		// t.DesiredState <= api.TaskStateRunning, but ignoring tasks
		// with DesiredState == NEW simplifies the drainer unit tests.
		if t.DesiredState >= api.TaskStateReady && t.DesiredState <= api.TaskStateRunning {
			runningTasks = append(runningTasks, t)
			runningInstances[t.Instance] = struct{}{}
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
			o.updater.Update(ctx, service, runningTasks)
			o.addTasks(ctx, batch, service, runningInstances, specifiedInstances-numTasks)

		case specifiedInstances < numTasks:
			// Update up to N tasks then remove the extra
			log.G(ctx).Debugf("Service %s was scaled down from %d to %d instances", service.ID, numTasks, specifiedInstances)
			o.updater.Update(ctx, service, runningTasks[:specifiedInstances])
			o.removeTasks(ctx, batch, service, runningTasks[specifiedInstances:])

		case specifiedInstances == numTasks:
			// Simple update, no scaling - update all tasks.
			o.updater.Update(ctx, service, runningTasks)
		}
		return nil
	})

	if err != nil {
		log.G(ctx).WithError(err).Errorf("reconcile batch failed")
	}
}

func (o *Orchestrator) addTasks(ctx context.Context, batch state.Batch, service *api.Service, runningInstances map[uint64]struct{}, count int) {
	instance := uint64(0)
	for i := 0; i < count; i++ {
		// Find an instance number that is missing a running task
		for {
			instance++
			if _, ok := runningInstances[instance]; !ok {
				break
			}
		}

		err := batch.Update(func(tx state.Tx) error {
			return tx.Tasks().Create(newTask(service, instance))
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
