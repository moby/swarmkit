package orchestrator

import (
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/protobuf/ptypes"
	"golang.org/x/net/context"
)

// This file provides task-level orchestration. It observes changes to task
// and node state and kills/recreates tasks if necessary. This is distinct from
// service-level reconcillation, which observes changes to services and creates
// and/or kills tasks to match the service definition.

func invalidNode(n *api.Node) bool {
	return n == nil ||
		n.Status.State != api.NodeStatus_READY ||
		n.Spec.Availability == api.NodeAvailabilityDrain
}

func (o *Orchestrator) initTasks(ctx context.Context, readTx state.ReadTx) error {
	tasks, err := readTx.Tasks().Find(state.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID != "" {
			n := readTx.Nodes().Get(t.NodeID)
			if invalidNode(n) && t.Status.State <= api.TaskStateRunning && t.DesiredState <= api.TaskStateRunning {
				o.restartTasks[t.ID] = struct{}{}
			}
		}
		if t.ServiceID != "" {
			service := readTx.Services().Get(t.ServiceID)
			if !isRelatedService(service) {
				o.restartTasks[t.ID] = struct{}{}
			}
		}
	}

	_, err = o.store.Batch(func(batch state.Batch) error {
		for _, t := range tasks {
			if t.ServiceID == "" || t.DesiredState != api.TaskStateReady {
				continue
			}
			service := readTx.Services().Get(t.ServiceID)
			if !isRelatedService(service) {
				continue
			}
			if service.Spec.Restart != nil && service.Spec.Restart.Delay != 0 {
				timestamp, err := ptypes.Timestamp(t.Status.Timestamp)
				if err == nil {
					restartTime := timestamp.Add(service.Spec.Restart.Delay)
					restartDelay := restartTime.Sub(time.Now())
					if restartDelay > service.Spec.Restart.Delay {
						restartDelay = service.Spec.Restart.Delay
					}
					if restartDelay > 0 {
						o.restarts.DelayStart(ctx, t.ID, restartDelay)
						continue
					}
				}
			}

			// Start now
			err := batch.Update(func(tx state.Tx) error {
				return o.restarts.StartNow(tx, t.ID)
			})
			if err != nil {
				log.G(ctx).WithError(err).WithField("task.id", t.ID).Error("moving task out of delayed state failed")
			}
		}
		return nil
	})

	return err
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
		if v.Task.DesiredState <= api.TaskStateRunning {
			service := o.resolveService(ctx, v.Task)
			if !isRelatedService(service) {
				return
			}
			o.reconcileServices[service.ID] = service
		}
		o.restarts.Cancel(v.Task.ID)
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
						if t.DesiredState > api.TaskStateRunning {
							return nil
						}

						service := tx.Services().Get(t.ServiceID)
						if !isRelatedService(service) {
							return nil
						}

						// Restart task if applicable
						if err := o.restarts.Restart(ctx, tx, service, *t); err != nil {
							return err
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
	var err error
	o.store.View(func(tx state.ReadTx) {
		var tasks []*api.Task
		tasks, err = tx.Tasks().Find(state.ByNodeID(nodeID))
		if err != nil {
			return
		}

		for _, t := range tasks {
			if t.DesiredState > api.TaskStateRunning {
				continue
			}
			service := tx.Services().Get(t.ServiceID)
			if isRelatedService(service) {
				o.restartTasks[t.ID] = struct{}{}
			}
		}
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to list tasks to remove")
	}
}

func (o *Orchestrator) handleNodeChange(ctx context.Context, n *api.Node) {
	if !invalidNode(n) {
		return
	}

	o.restartTasksByNodeID(ctx, n.ID)
}

func (o *Orchestrator) handleTaskChange(ctx context.Context, t *api.Task) {
	// If we already set the desired state past TaskStateRunning, there is no
	// further action necessary.
	if t.DesiredState > api.TaskStateRunning {
		return
	}

	var (
		n       *api.Node
		service *api.Service
	)
	o.store.View(func(tx state.ReadTx) {
		if t.NodeID != "" {
			n = tx.Nodes().Get(t.NodeID)
		}
		if t.ServiceID != "" {
			service = tx.Services().Get(t.ServiceID)
		}
	})

	if !isRelatedService(service) {
		return
	}

	if t.Status.TerminalState > api.TaskStateNew ||
		(t.NodeID != "" && invalidNode(n)) ||
		(t.ServiceID != "" && service == nil) {
		o.restartTasks[t.ID] = struct{}{}
	}
}
