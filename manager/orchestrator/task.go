package orchestrator

import (
	"reflect"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	gogotypes "github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
)

// Restarter defines an interface to start or delay start tasks
type restarter interface {
	StartNow(tx store.Tx, taskID string) error
	DelayStart(ctx context.Context, _ store.Tx, oldTask *api.Task, newTaskID string, delay time.Duration, waitStop bool) <-chan struct{}
}

// DefaultRestartDelay is the restart delay value to use when none is
// specified.
const DefaultRestartDelay = 5 * time.Second

// NewTask creates a new task.
func NewTask(cluster *api.Cluster, service *api.Service, slot uint64, nodeID string) *api.Task {
	var logDriver *api.Driver
	if service.Spec.Task.LogDriver != nil {
		// use the log driver specific to the task, if we have it.
		logDriver = service.Spec.Task.LogDriver
	} else if cluster != nil {
		// pick up the cluster default, if available.
		logDriver = cluster.Spec.TaskDefaults.LogDriver // nil is okay here.
	}

	taskID := identity.NewID()
	task := api.Task{
		ID:                 taskID,
		ServiceAnnotations: service.Spec.Annotations,
		Spec:               service.Spec.Task,
		ServiceID:          service.ID,
		Slot:               slot,
		Status: api.TaskStatus{
			State:     api.TaskStateNew,
			Timestamp: ptypes.MustTimestampProto(time.Now()),
			Message:   "created",
		},
		Endpoint: &api.Endpoint{
			Spec: service.Spec.Endpoint.Copy(),
		},
		DesiredState: api.TaskStateRunning,
		LogDriver:    logDriver,
	}

	// In global mode we also set the NodeID
	if nodeID != "" {
		task.NodeID = nodeID
	}

	return &task
}

// RestartCondition returns the restart condition to apply to this task.
func RestartCondition(task *api.Task) api.RestartPolicy_RestartCondition {
	restartCondition := api.RestartOnAny
	if task.Spec.Restart != nil {
		restartCondition = task.Spec.Restart.Condition
	}
	return restartCondition
}

// IsTaskDirty determines whether a task matches the given service's spec.
func IsTaskDirty(s *api.Service, t *api.Task) bool {
	return !reflect.DeepEqual(s.Spec.Task, t.Spec) ||
		(t.Endpoint != nil && !reflect.DeepEqual(s.Spec.Endpoint, t.Endpoint.Spec))
}

// InvalidNode is true if the node is nil, down, or Drain
func InvalidNode(n *api.Node) bool {
	return n == nil ||
		n.Status.State == api.NodeStatus_DOWN ||
		n.Spec.Availability == api.NodeAvailabilityDrain
}

type isRelatedService func(*api.Service) bool

// InitTasks fix tasks in store before orchestrator run. Previous manager might haven't finished processing their updates.
func InitTasks(ctx context.Context, s *store.MemoryStore, readTx store.ReadTx, relatedService isRelatedService, restart restarter) error {
	_, err := s.Batch(func(batch *store.Batch) error {
		tasks, err := store.FindTasks(readTx, store.All)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.ServiceID == "" {
				continue
			}

			// TODO(aluzzardi): We should NOT retrieve the service here.
			service := store.GetService(readTx, t.ServiceID)
			if service == nil {
				// Service was deleted
				err := batch.Update(func(tx store.Tx) error {
					return store.DeleteTask(tx, t.ID)
				})
				if err != nil {
					log.G(ctx).WithError(err).Error("failed to set task desired state to dead")
				}
				continue
			}
			// only handle tasks belong to related service type
			if !relatedService(service) {
				continue
			}
			// if the task shouldn't be run on the specified node, set the task to shutdown
			// so orchestrator can work on it
			if t.NodeID != "" {
				n := store.GetNode(readTx, t.NodeID)
				if InvalidNode(n) && t.DesiredState <= api.TaskStateRunning {
					t.DesiredState = api.TaskStateShutdown
					err = batch.Update(func(tx store.Tx) error {
						return store.UpdateTask(tx, t)
					})
					if err != nil {
						log.G(ctx).WithError(err).Error("failed to update task")
					}
					continue
				}
			}
			// if a task failed but desired state is alive, trigger update so orchestrator
			// can look at it
			if t.DesiredState <= api.TaskStateRunning && t.Status.State > api.TaskStateRunning {
				err := batch.Update(func(tx store.Tx) error {
					return store.UpdateTask(tx, t)
				})
				if err != nil {
					log.G(ctx).WithError(err).Error("failed to update task")
				}
				continue
			}
			// if a task's desired state is ready, orchestrator may need to start it
			if t.DesiredState != api.TaskStateReady {
				continue
			}
			// respect restart delay
			restartDelay := DefaultRestartDelay
			if t.Spec.Restart != nil && t.Spec.Restart.Delay != nil {
				var err error
				restartDelay, err = gogotypes.DurationFromProto(t.Spec.Restart.Delay)
				if err != nil {
					log.G(ctx).WithError(err).Error("invalid restart delay")
					restartDelay = DefaultRestartDelay
				}
			}
			if restartDelay != 0 {
				timestamp, err := gogotypes.TimestampFromProto(t.Status.Timestamp)
				if err == nil {
					restartTime := timestamp.Add(restartDelay)
					calculatedRestartDelay := restartTime.Sub(time.Now())
					if calculatedRestartDelay < restartDelay {
						restartDelay = calculatedRestartDelay
					}
					if restartDelay > 0 {
						_ = batch.Update(func(tx store.Tx) error {
							t := store.GetTask(tx, t.ID)
							// TODO(aluzzardi): This is shady as well. We should have a more generic condition.
							if t == nil || t.DesiredState != api.TaskStateReady {
								return nil
							}
							restart.DelayStart(ctx, tx, nil, t.ID, restartDelay, true)
							return nil
						})
						continue
					}
				} else {
					log.G(ctx).WithError(err).Error("invalid status timestamp")
				}
			}

			// Start now
			err := batch.Update(func(tx store.Tx) error {
				return restart.StartNow(tx, t.ID)
			})
			if err != nil {
				log.G(ctx).WithError(err).WithField("task.id", t.ID).Error("moving task out of delayed state failed")
			}
		}
		return nil
	})

	return err
}
