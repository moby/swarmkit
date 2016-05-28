package orchestrator

import (
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
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

func (r *ReplicatedOrchestrator) initTasks(ctx context.Context, readTx store.ReadTx) error {
	tasks, err := store.FindTasks(readTx, store.All)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if t.NodeID != "" {
			n := store.GetNode(readTx, t.NodeID)
			if invalidNode(n) && t.Status.State <= api.TaskStateRunning && t.DesiredState <= api.TaskStateRunning {
				r.restartTasks[t.ID] = struct{}{}
			}
		}
	}

	_, err = r.store.Batch(func(batch *store.Batch) error {
		for _, t := range tasks {
			if t.ServiceID == "" {
				continue
			}

			service := store.GetService(readTx, t.ServiceID)
			if service == nil {
				// Service was deleted
				err := batch.Update(func(tx store.Tx) error {
					return store.DeleteTask(tx, t.ID)
				})
				if err != nil {
					log.G(ctx).WithError(err).Errorf("failed to set task desired state to dead")
				}
				continue
			}
			if t.DesiredState != api.TaskStateReady || !isReplicatedService(service) {
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
						_ = batch.Update(func(tx store.Tx) error {
							t := store.GetTask(tx, t.ID)
							if t == nil || t.DesiredState != api.TaskStateReady {
								return nil
							}
							r.restarts.DelayStart(ctx, tx, service, nil, t.ID, restartDelay, true)
							return nil
						})
						continue
					}
				}
			}

			// Start now
			err := batch.Update(func(tx store.Tx) error {
				return r.restarts.StartNow(tx, t.ID)
			})
			if err != nil {
				log.G(ctx).WithError(err).WithField("task.id", t.ID).Error("moving task out of delayed state failed")
			}
		}
		return nil
	})

	return err
}

func (r *ReplicatedOrchestrator) handleTaskEvent(ctx context.Context, event events.Event) {
	switch v := event.(type) {
	case state.EventDeleteNode:
		r.restartTasksByNodeID(ctx, v.Node.ID)
	case state.EventCreateNode:
		r.handleNodeChange(ctx, v.Node)
	case state.EventUpdateNode:
		r.handleNodeChange(ctx, v.Node)
	case state.EventDeleteTask:
		if v.Task.DesiredState <= api.TaskStateRunning {
			service := r.resolveService(ctx, v.Task)
			if !isReplicatedService(service) {
				return
			}
			r.reconcileServices[service.ID] = service
		}
		r.restarts.Cancel(v.Task.ID)
	case state.EventUpdateTask:
		r.handleTaskChange(ctx, v.Task)
	case state.EventCreateTask:
		r.handleTaskChange(ctx, v.Task)
	}
}

func (r *ReplicatedOrchestrator) tickTasks(ctx context.Context) {
	if len(r.restartTasks) > 0 {
		_, err := r.store.Batch(func(batch *store.Batch) error {
			for taskID := range r.restartTasks {
				err := batch.Update(func(tx store.Tx) error {
					// TODO(aaronl): optimistic update?
					t := store.GetTask(tx, taskID)
					if t != nil {
						if t.DesiredState > api.TaskStateRunning {
							return nil
						}

						service := store.GetService(tx, t.ServiceID)
						if !isReplicatedService(service) {
							return nil
						}

						// Restart task if applicable
						if err := r.restarts.Restart(ctx, tx, service, *t); err != nil {
							return err
						}
					}
					return nil
				})
				if err != nil {
					log.G(ctx).WithError(err).Errorf("ReplicatedOrchestrator task reaping transaction failed")
				}
			}
			return nil
		})

		if err != nil {
			log.G(ctx).WithError(err).Errorf("orchestator task removal batch failed")
		}

		r.restartTasks = make(map[string]struct{})
	}
}

func (r *ReplicatedOrchestrator) restartTasksByNodeID(ctx context.Context, nodeID string) {
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
			if isReplicatedService(service) {
				r.restartTasks[t.ID] = struct{}{}
			}
		}
	})
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to list tasks to remove")
	}
}

func (r *ReplicatedOrchestrator) handleNodeChange(ctx context.Context, n *api.Node) {
	if !invalidNode(n) {
		return
	}

	r.restartTasksByNodeID(ctx, n.ID)
}

func (r *ReplicatedOrchestrator) handleTaskChange(ctx context.Context, t *api.Task) {
	// If we already set the desired state past TaskStateRunning, there is no
	// further action necessary.
	if t.DesiredState > api.TaskStateRunning {
		return
	}

	var (
		n       *api.Node
		service *api.Service
	)
	r.store.View(func(tx store.ReadTx) {
		if t.NodeID != "" {
			n = store.GetNode(tx, t.NodeID)
		}
		if t.ServiceID != "" {
			service = store.GetService(tx, t.ServiceID)
		}
	})

	if !isReplicatedService(service) {
		return
	}

	if t.Status.State > api.TaskStateRunning ||
		(t.NodeID != "" && invalidNode(n)) {
		r.restartTasks[t.ID] = struct{}{}
	}
}
