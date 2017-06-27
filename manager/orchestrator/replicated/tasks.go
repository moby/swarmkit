package replicated

import (
	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/orchestrator/taskinit"
	"github.com/docker/swarmkit/manager/state/store"
	"golang.org/x/net/context"
)

// This file provides task-level orchestration. It observes changes to task
// and node state and kills/recreates tasks if necessary. This is distinct from
// service-level reconciliation, which observes changes to services and creates
// and/or kills tasks to match the service definition.

func (r *Orchestrator) initTasks(ctx context.Context, readTx store.ReadTx) error {
	return taskinit.CheckTasks(ctx, r.store, readTx, r, r.restarts)
}

func (r *Orchestrator) handleTaskEvent(ctx context.Context, event events.Event) {
	switch v := event.(type) {
	case api.EventDeleteNode:
		r.restartTasksByNodeID(ctx, v.Node.ID, true)
	case api.EventCreateNode:
		r.handleNodeChange(ctx, v.Node)
	case api.EventUpdateNode:
		r.handleNodeChange(ctx, v.Node)
	case api.EventDeleteTask:
		if v.Task.DesiredState <= api.TaskStateRunning {
			service := r.resolveService(ctx, v.Task)
			if !orchestrator.IsReplicatedService(service) {
				return
			}
			r.reconcileServices[service.ID] = service
		}
		r.restarts.Cancel(v.Task.ID)
	case api.EventUpdateTask:
		r.handleTaskChange(ctx, v.Task)
	case api.EventCreateTask:
		r.handleTaskChange(ctx, v.Task)
	}
}

func (r *Orchestrator) tickTasks(ctx context.Context) {
	if len(r.restartTasks) > 0 {
		err := r.store.Batch(func(batch *store.Batch) error {
			for taskID, forceShutdownState := range r.restartTasks {
				err := batch.Update(func(tx store.Tx) error {
					// TODO(aaronl): optimistic update?
					t := store.GetTask(tx, taskID)
					if t != nil {
						if t.DesiredState > api.TaskStateRunning {
							return nil
						}

						service := store.GetService(tx, t.ServiceID)
						if !orchestrator.IsReplicatedService(service) {
							return nil
						}

						// Restart task if applicable
						if err := r.restarts.Restart(ctx, tx, r.cluster, service, *t, forceShutdownState); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					log.G(ctx).WithError(err).Errorf("Orchestrator task reaping transaction failed")
				}
			}
			return nil
		})

		if err != nil {
			log.G(ctx).WithError(err).Errorf("orchestrator task removal batch failed")
		}

		r.restartTasks = make(map[string]bool)
	}
}

func (r *Orchestrator) restartTasksByNodeID(ctx context.Context, nodeID string, forceShutdownState bool) {
	var err error
	r.store.View(func(tx store.ReadTx) {
		var tasks []*api.Task
		tasks, err = store.FindTasks(tx, store.ByNodeID(nodeID))
		if err != nil {
			return
		}

		for _, t := range tasks {
			if t.DesiredState > api.TaskStateRunning {
				continue
			}
			service := store.GetService(tx, t.ServiceID)
			if orchestrator.IsReplicatedService(service) {
				r.restartTasks[t.ID] = forceShutdownState
			}
		}
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to list tasks to remove")
	}
}

func (r *Orchestrator) handleNodeChange(ctx context.Context, n *api.Node) {
	if n.Spec.Availability == api.NodeAvailabilityDrain {
		r.restartTasksByNodeID(ctx, n.ID, true)
	} else if n.Status.State == api.NodeStatus_DOWN {
		r.restartTasksByNodeID(ctx, n.ID, false)
	}
}

// handleTaskChange defines what orchestrator does when a task is updated by agent.
func (r *Orchestrator) handleTaskChange(ctx context.Context, t *api.Task) {
	// If we already set the desired state past TaskStateRunning, there is no
	// further action necessary.
	if t.DesiredState > api.TaskStateRunning {
		return
	}

	var (
		node    *api.Node
		service *api.Service
	)
	r.store.View(func(tx store.ReadTx) {
		if t.NodeID != "" {
			node = store.GetNode(tx, t.NodeID)
		}
		if t.ServiceID != "" {
			service = store.GetService(tx, t.ServiceID)
		}
	})

	r.maybeRestartTask(t, service, node)
}

func (r *Orchestrator) maybeRestartTask(t *api.Task, service *api.Service, node *api.Node) {
	if !orchestrator.IsReplicatedService(service) {
		return
	}

	if t.Status.State > api.TaskStateRunning {
		r.restartTasks[t.ID] = false
	}
	if t.NodeID != "" {
		if node == nil {
			r.restartTasks[t.ID] = false
		} else if node.Spec.Availability == api.NodeAvailabilityDrain {
			r.restartTasks[t.ID] = true
		} else if node.Status.State == api.NodeStatus_DOWN {
			r.restartTasks[t.ID] = false
		}
	}
}

// FixTask validates a task with the current cluster settings, and takes
// action to make it conformant. it's called at orchestrator initialization.
func (r *Orchestrator) FixTask(ctx context.Context, batch *store.Batch, t *api.Task) {
	// If we already set the desired state past TaskStateRunning, there is no
	// further action necessary.
	if t.DesiredState > api.TaskStateRunning {
		return
	}

	var (
		node    *api.Node
		service *api.Service
	)
	batch.Update(func(tx store.Tx) error {
		if t.NodeID != "" {
			node = store.GetNode(tx, t.NodeID)
		}
		if t.ServiceID != "" {
			service = store.GetService(tx, t.ServiceID)
		}
		return nil
	})

	r.maybeRestartTask(t, service, node)
}
