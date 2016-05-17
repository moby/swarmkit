package orchestrator

import (
	"sync"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
)

// RestartSupervisor initiates and manages restarts. It's responsible for
// delaying restarts when applicable.
type RestartSupervisor struct {
	mu       sync.Mutex
	store    *store.MemoryStore
	delays   map[string]*time.Timer
	delaysWG sync.WaitGroup
}

// NewRestartSupervisor creates a new RestartSupervisor.
func NewRestartSupervisor(store *store.MemoryStore) *RestartSupervisor {
	return &RestartSupervisor{
		store:  store,
		delays: make(map[string]*time.Timer),
	}
}

// Restart initiates a new task to replace t if appropriate under the service's
// restart policy.
func (r *RestartSupervisor) Restart(ctx context.Context, tx store.Tx, service *api.Service, t api.Task, drained bool) error {
	t.DesiredState = api.TaskStateDead
	err := store.UpdateTask(tx, &t)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to set task desired state to dead")
		return err
	}

	condition := restartCondition(service)

	if condition != api.RestartAlways &&
		(condition != api.RestartOnFailure || t.Status.TerminalState == api.TaskStateCompleted) {
		return nil
	}

	var restartTask *api.Task

	switch service.Spec.Mode {
	case api.ServiceModeRunning:
		restartTask = newTask(service, t.Instance)
	case api.ServiceModeFill:
		restartTask = newTask(service, 0)
		restartTask.NodeID = t.NodeID
	default:
		log.G(ctx).Error("service mode not supported by restart supervisor")
		return nil
	}

	if !drained && service.Spec.Restart != nil && service.Spec.Restart.Delay != 0 {
		restartTask.DesiredState = api.TaskStateReady
	}
	if err := store.CreateTask(tx, restartTask); err != nil {
		log.G(ctx).WithError(err).WithField("task.id", restartTask.ID).Error("task create failed")
		return err
	}
	if restartTask.DesiredState == api.TaskStateReady {
		r.DelayStart(ctx, restartTask.ID, service.Spec.Restart.Delay)
	}
	return nil
}

// DelayStart starts a timer that starts the task after a specified delay.
func (r *RestartSupervisor) DelayStart(ctx context.Context, taskID string, delay time.Duration) {
	r.mu.Lock()
	r.delaysWG.Add(1)
	timer := time.AfterFunc(delay,
		func() {
			err := r.store.Update(func(tx store.Tx) error {
				err := r.StartNow(tx, taskID)
				if err != nil {
					log.G(ctx).WithError(err).WithField("task.id", taskID).Error("moving task out of delayed state failed")
				}
				return nil
			})
			if err != nil {
				log.G(ctx).WithError(err).WithField("task.id", taskID).Error("task restart transaction failed")
			}
			r.mu.Lock()
			delete(r.delays, taskID)
			r.mu.Unlock()
			r.delaysWG.Done()
		})

	if oldTimer, ok := r.delays[taskID]; ok {
		// Task ID collision should be extremely rare
		log.G(ctx).WithField("task.id", taskID).Error("start delay timer already exists for this task ID - replacing")
		if oldTimer.Stop() {
			r.delaysWG.Done()
		}
	}
	r.delays[taskID] = timer
	r.mu.Unlock()
}

// StartNow moves the task into the RUNNING state so it will proceed to start
// up.
func (r *RestartSupervisor) StartNow(tx store.Tx, taskID string) error {
	t := store.GetTask(tx, taskID)
	if t == nil || t.DesiredState > api.TaskStateReady {
		return nil
	}
	t.DesiredState = api.TaskStateRunning
	return store.UpdateTask(tx, t)
}

// Cancel cancels a pending restart.
func (r *RestartSupervisor) Cancel(taskID string) {
	r.mu.Lock()
	if timer, ok := r.delays[taskID]; ok && timer.Stop() {
		r.delaysWG.Done()
		delete(r.delays, taskID)
	}
	r.mu.Unlock()
}

// CancelAll aborts all pending restarts and waits for any instances of
// StartNow that have already triggered to complete.
func (r *RestartSupervisor) CancelAll() {
	r.mu.Lock()
	for _, timer := range r.delays {
		if timer.Stop() {
			r.delaysWG.Done()
		}
	}
	r.delays = make(map[string]*time.Timer)
	r.mu.Unlock()
	r.delaysWG.Wait()
}
