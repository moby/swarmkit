package managerservice

import (
	"sort"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/manager/constraint"
	"golang.org/x/net/context"
)

// This file provices service-level orchestration. It observes changes to
// services and creates and destroys tasks as necessary to match the service
// specifications. This is different from task-level orchestration, which
// responds to changes in individual tasks (or nodes which run them).

func (r *Orchestrator) initCluster(readTx store.ReadTx) error {
	clusters, err := store.FindClusters(readTx, store.ByName(store.DefaultClusterName))
	if err != nil {
		return err
	}

	if len(clusters) != 1 {
		// we'll just pick it when it is created.
		return nil
	}

	r.cluster = clusters[0]
	return nil
}

func (r *Orchestrator) initServices(readTx store.ReadTx) error {
	services, err := store.FindServices(readTx, store.All)
	if err != nil {
		return err
	}
	for _, s := range services {
		if orchestrator.IsSwarmManagerService(s) {
			r.reconcileServices[s.ID] = s
		}
	}
	return nil
}

func (r *Orchestrator) handleServiceEvent(ctx context.Context, event events.Event) {
	switch v := event.(type) {
	case api.EventDeleteService:
		if !orchestrator.IsSwarmManagerService(v.Service) {
			return
		}
		orchestrator.SetServiceTasksRemove(ctx, r.store, v.Service)
		r.restarts.ClearServiceHistory(v.Service.ID)
		delete(r.reconcileServices, v.Service.ID)
	case api.EventCreateService:
		if !orchestrator.IsSwarmManagerService(v.Service) {
			return
		}
		r.reconcileServices[v.Service.ID] = v.Service
	case api.EventUpdateService:
		if !orchestrator.IsSwarmManagerService(v.Service) {
			return
		}
		r.reconcileServices[v.Service.ID] = v.Service
	}
}

func (r *Orchestrator) tickServices(ctx context.Context) {
	if len(r.reconcileServices) > 0 {
		for _, s := range r.reconcileServices {
			r.reconcile(ctx, s)
		}
		r.reconcileServices = make(map[string]*api.Service)
	}
}

func (r *Orchestrator) resolveService(ctx context.Context, task *api.Task) *api.Service {
	if task.ServiceID == "" {
		return nil
	}
	var service *api.Service
	r.store.View(func(tx store.ReadTx) {
		service = store.GetService(tx, task.ServiceID)
	})
	return service
}

// reconcile is duplicated from Replicated and Global, and uses the same TaskTemplate,
// however it does not use the notion of slots, and simply lists the currently-running
// Manager nodes directly from the API. Having an orchestrator orchestrate its own state
// caused a ton of circular dependencies, and the real solution should be to somehow "ingest"
// an existing state of services not managed by the orchestrator into its own managed domain,
// but I needed a proof-of-concept so I bypassed slots and read raft.state directly,
// in a orchestrator.stateless manner.
func (r *Orchestrator) reconcile(ctx context.Context, service *api.Service) {
	managers, err = FindNodes(readTx, ByRole(api.NodeRoleManager))
	numSlots = len(managers)

	deploy := service.Spec.GetMode().(*api.ServiceSpec_Manager)
	specifiedSlots := deploy.Manager.Replicas


	switch {

		case specifiedSlots > uint64(numSlots):
			log.G(ctx).Debugf("Service %s was scaled up from %d to %d instances", service.ID, numSlots, specifiedSlots)
			// Generate a list of non-Manager nodes.
			nodes, err = FindNodes(readTx, ByRole(api.NodeRoleWorker))
			// Check for specified Placement.Constraints and remove ineligible nodes.
			if service.Spec.Task.Placement != nil && len(service.Spec.Task.Placement.Constraints) != 0 {
				constraints, _ = constraint.Parse(service.Spec.Task.Placement.Constraints)
				for _, node := range nodes {
					if !constraint.NodeMatches(constraints, node) {
						nodes[node] = nodes[len(nodes)-1]
						nodes[len(nodes)-1] = nil
						nodes = nodes[:len(nodes)-1]
					}
				}
			}
			// TODO sort nodes[] by Placement.Preferences, descending.
			// TODO within each Preference group, sort nodes[] by resource usage, ascending.


			// Promote nodes[0] to Manager.

		case specifiedSlots < uint64(numSlots):
			log.G(ctx).Debugf("Service %s was scaled down from %d to %d instances", service.ID, numSlots, specifiedSlots)
			// TODO if !quorum, wait some specified time for failed Managers to restart
			// TODO remove the current leader from the managers[] cull list.
			// TODO sort managers[] by Placement.Preferences, ascending.
			// TODO within each Preference group, sort managers[] by resource usage, descending.

			// Demote managers[0] to Worker.

}

// Initial version of Swarm Orchestrator does not use tasks, but should in the future
// to add additional functionality like notification and node draining to the simple
// Promote/Demote scheme, possible adding an "Exec" or "shell" runtime, or whatever.
// So probably best not to remove unused task functions duplicated from Replicated and Global.

func (r *Orchestrator) addTasks(ctx context.Context, batch *store.Batch, service *api.Service, runningSlots map[uint64]orchestrator.Slot, deadSlots map[uint64]orchestrator.Slot, count uint64) {
	slot := uint64(0)
	for i := uint64(0); i < count; i++ {
		// Find a slot number that is missing a running task
		for {
			slot++
			if _, ok := runningSlots[slot]; !ok {
				break
			}
		}

		delete(deadSlots, slot)
		err := batch.Update(func(tx store.Tx) error {
			return store.CreateTask(tx, orchestrator.NewTask(r.cluster, service, slot, ""))
		})
		if err != nil {
			log.G(ctx).Errorf("Failed to create task: %v", err)
		}
	}
}

// setTasksDesiredState sets the desired state for all tasks for the given slots to the
// requested state
func (r *Orchestrator) setTasksDesiredState(ctx context.Context, batch *store.Batch, slots []orchestrator.Slot, newDesiredState api.TaskState) {
	for _, slot := range slots {
		for _, t := range slot {
			err := batch.Update(func(tx store.Tx) error {
				// time travel is not allowed. if the current desired state is
				// above the one we're trying to go to we can't go backwards.
				// we have nothing to do and we should skip to the next task
				if t.DesiredState > newDesiredState {
					// log a warning, though. we shouln't be trying to rewrite
					// a state to an earlier state
					log.G(ctx).Warnf(
						"cannot update task %v in desired state %v to an earlier desired state %v",
						t.ID, t.DesiredState, newDesiredState,
					)
					return nil
				}
				// update desired state
				t.DesiredState = newDesiredState

				return store.UpdateTask(tx, t)
			})

			// log an error if we get one
			if err != nil {
				log.G(ctx).WithError(err).Errorf("failed to update task to %v", newDesiredState.String())
			}
		}
	}
}

func (r *Orchestrator) deleteTasksMap(ctx context.Context, batch *store.Batch, slots map[uint64]orchestrator.Slot) {
	for _, slot := range slots {
		for _, t := range slot {
			r.deleteTask(ctx, batch, t)
		}
	}
}

func (r *Orchestrator) deleteTask(ctx context.Context, batch *store.Batch, t *api.Task) {
	err := batch.Update(func(tx store.Tx) error {
		return store.DeleteTask(tx, t.ID)
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("deleting task %s failed", t.ID)
	}
}

// IsRelatedService returns true if the service should be governed by this orchestrator
func (r *Orchestrator) IsRelatedService(service *api.Service) bool {
	return orchestrator.IsSwarmManagerService(service)
}
