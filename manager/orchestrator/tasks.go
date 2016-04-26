package orchestrator

import (
	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
)

// This file provides task-level orchestration. It observes changes to task
// and node state and kills/recreates tasks if necessary. This is distinct from
// service-level reconcillation, which observes changes to services and creates
// and/or kills tasks to match the service definition.

func invalidNode(n *api.Node) bool {
	return n == nil ||
		n.Status.State != api.NodeStatus_READY ||
		(n.Spec != nil && n.Spec.Availability == api.NodeAvailabilityDrain)
}

func (o *Orchestrator) initTasks(readTx state.ReadTx) error {
	tasks, err := readTx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID != "" {
			n := readTx.Nodes().Get(t.NodeID)
			if invalidNode(n) && (t.Status == nil || t.Status.State != api.TaskStateDead) && t.DesiredState != api.TaskStateDead {
				o.restartTasks[t.ID] = struct{}{}
			}
		}
	}
	return nil
}

func (o *Orchestrator) handleTaskEvent(ctx context.Context, event events.Event) {
	switch v := event.(type) {
	case state.EventDeleteNode:
		o.restartTasksByNodeID(ctx, v.Node.ID)
	case state.EventCreateNode:
		o.handleNodeChange(ctx, v.Node)
	case state.EventUpdateNode:
		o.handleNodeChange(ctx, v.Node)
	case state.EventDeleteTask:
		if v.Task.DesiredState == api.TaskStateRunning {
			service := o.resolveService(ctx, v.Task)
			if !isRelatedService(service) {
				return
			}
			o.reconcileServices[service.ID] = service
		}
	case state.EventUpdateTask:
		o.handleTaskChange(ctx, v.Task)
	case state.EventCreateTask:
		o.handleTaskChange(ctx, v.Task)
	}
}

func (o *Orchestrator) tickTasks(ctx context.Context) {
	if len(o.restartTasks) > 0 {
		_, err := o.store.Batch(func(batch state.Batch) error {
			for taskID := range o.restartTasks {
				err := batch.Update(func(tx state.Tx) error {
					// TODO(aaronl): optimistic update?
					t := tx.Tasks().Get(taskID)
					if t != nil {
						service := tx.Services().Get(t.ServiceID)
						if !isRelatedService(service) {
							return nil
						}

						t.DesiredState = api.TaskStateDead
						err := tx.Tasks().Update(t)
						if err != nil {
							log.G(ctx).WithError(err).Errorf("failed to set task desired state to dead")
							return err
						}

						// Restart task if applicable
						switch restartCondition(service) {
						case api.RestartAlways:
							return tx.Tasks().Create(newTask(service, t.Instance))
						case api.RestartOnFailure:
							if t.Status != nil && t.Status.TerminalState != api.TaskStateCompleted {
								return tx.Tasks().Create(newTask(service, t.Instance))
							}
						}
					}
					return nil
				})
				if err != nil {
					log.G(ctx).WithError(err).Errorf("orchestrator task reaping transaction failed")
				}
			}
			return nil
		})

		if err != nil {
			log.G(ctx).WithError(err).Errorf("orchestator task removal batch failed")
		}

		o.restartTasks = make(map[string]struct{})
	}
}

func (o *Orchestrator) restartTasksByNodeID(ctx context.Context, nodeID string) {
	err := o.store.View(func(tx state.ReadTx) error {
		tasks, err := tx.Tasks().Find(state.ByNodeID(nodeID))
		if err != nil {
			return err
		}

		for _, t := range tasks {
			service := tx.Services().Get(t.ServiceID)
			if isRelatedService(service) {
				o.restartTasks[t.ID] = struct{}{}
			}
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("orchestrator transaction failed listing tasks to remove")
	}
}

func (o *Orchestrator) handleNodeChange(ctx context.Context, n *api.Node) {
	if !invalidNode(n) {
		return
	}

	o.restartTasksByNodeID(ctx, n.ID)
}

func (o *Orchestrator) handleTaskChange(ctx context.Context, t *api.Task) {
	// If we already set the desired state to TaskStateDead, there is no
	// further action necessary.
	if t.DesiredState == api.TaskStateDead {
		return
	}

	var (
		n       *api.Node
		service *api.Service
	)
	err := o.store.View(func(tx state.ReadTx) error {
		if t.NodeID != "" {
			n = tx.Nodes().Get(t.NodeID)
		}
		if t.ServiceID != "" {
			service = tx.Services().Get(t.ServiceID)
		}
		return nil
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("orchestrator transaction failed getting tasks")
		return
	}

	if !isRelatedService(service) {
		return
	}

	if (t.Status != nil && t.Status.TerminalState != api.TaskStateNew) ||
		(t.NodeID != "" && invalidNode(n)) ||
		(t.ServiceID != "" && service == nil) {
		o.restartTasks[t.ID] = struct{}{}
	}
}
