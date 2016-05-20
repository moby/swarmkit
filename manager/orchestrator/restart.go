package orchestrator

import (
	"container/list"
	"sync"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
)

type restartedInstance struct {
	timestamp time.Time
}

type instanceRestartInfo struct {
	// counter of restarts for this instance.
	totalRestarts uint64
	// Linked list of restartedInstance structs. Only used when
	// Restart.MaxAttempts and Restart.Window are both
	// nonzero.
	restartedInstances *list.List
}

// RestartSupervisor initiates and manages restarts. It's responsible for
// delaying restarts when applicable.
type RestartSupervisor struct {
	mu               sync.Mutex
	store            *store.MemoryStore
	delays           map[string]*time.Timer
	timersWG         sync.WaitGroup
	history          map[instanceTuple]*instanceRestartInfo
	historyByService map[string]map[instanceTuple]struct{}
}

// NewRestartSupervisor creates a new RestartSupervisor.
func NewRestartSupervisor(store *store.MemoryStore) *RestartSupervisor {
	return &RestartSupervisor{
		store:            store,
		delays:           make(map[string]*time.Timer),
		history:          make(map[instanceTuple]*instanceRestartInfo),
		historyByService: make(map[string]map[instanceTuple]struct{}),
	}
}

// Restart initiates a new task to replace t if appropriate under the service's
// restart policy.
func (r *RestartSupervisor) Restart(ctx context.Context, tx store.Tx, service *api.Service, t api.Task) error {
	t.DesiredState = api.TaskStateShutdown
	err := store.UpdateTask(tx, &t)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to set task desired state to dead")
		return err
	}

	if !r.shouldRestart(&t, service) {
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

	if service.Spec.Restart != nil && service.Spec.Restart.Delay != 0 {
		restartTask.DesiredState = api.TaskStateReady
	}
	if err := store.CreateTask(tx, restartTask); err != nil {
		log.G(ctx).WithError(err).WithField("task.id", restartTask.ID).Error("task create failed")
		return err
	}

	r.recordRestartHistory(restartTask, service)

	if restartTask.DesiredState == api.TaskStateReady {
		r.DelayStart(ctx, restartTask.ID, service.Spec.Restart.Delay)
	}
	return nil
}

func (r *RestartSupervisor) shouldRestart(t *api.Task, service *api.Service) bool {
	condition := restartCondition(service)

	if condition != api.RestartOnAny &&
		(condition != api.RestartOnFailure || t.Status.TerminalState == api.TaskStateCompleted) {
		return false
	}

	if service.Spec.Restart == nil || service.Spec.Restart.MaxAttempts == 0 {
		return true
	}

	instanceTuple := instanceTuple{
		instance:  t.Instance,
		serviceID: t.ServiceID,
	}

	// Instance is not meaningful for "fill" tasks, so they need to be
	// indexed by NodeID.
	if service.Spec.Mode == api.ServiceModeFill {
		instanceTuple.nodeID = t.NodeID
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	restartInfo := r.history[instanceTuple]
	if restartInfo == nil {
		return true
	}

	if service.Spec.Restart.Window == 0 {
		return restartInfo.totalRestarts < service.Spec.Restart.MaxAttempts
	}

	if restartInfo.restartedInstances == nil {
		return true
	}

	lookback := time.Now().Add(-service.Spec.Restart.Window)

	var next *list.Element
	for e := restartInfo.restartedInstances.Front(); e != nil; e = next {
		next = e.Next()

		if e.Value.(restartedInstance).timestamp.After(lookback) {
			break
		}
		restartInfo.restartedInstances.Remove(e)
	}

	numRestarts := uint64(restartInfo.restartedInstances.Len())

	if numRestarts == 0 {
		restartInfo.restartedInstances = nil
	}

	return numRestarts < service.Spec.Restart.MaxAttempts
}

func (r *RestartSupervisor) recordRestartHistory(restartTask *api.Task, service *api.Service) {
	if service.Spec.Restart == nil || service.Spec.Restart.MaxAttempts == 0 {
		// No limit on the number of restarts, so no need to record
		// history.
		return
	}
	tuple := instanceTuple{
		instance:  restartTask.Instance,
		serviceID: restartTask.ServiceID,
		nodeID:    restartTask.NodeID,
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.history[tuple] == nil {
		r.history[tuple] = &instanceRestartInfo{}
	}
	restartInfo := r.history[tuple]
	restartInfo.totalRestarts++

	if r.historyByService[restartTask.ServiceID] == nil {
		r.historyByService[restartTask.ServiceID] = make(map[instanceTuple]struct{})
	}
	r.historyByService[restartTask.ServiceID][tuple] = struct{}{}

	if service.Spec.Restart.Window != 0 {
		if restartInfo.restartedInstances == nil {
			restartInfo.restartedInstances = list.New()
		}

		restartedInstance := restartedInstance{
			timestamp: time.Now(),
		}

		restartInfo.restartedInstances.PushBack(restartedInstance)
	}
}

// DelayStart starts a timer that starts the task after a specified delay.
func (r *RestartSupervisor) DelayStart(ctx context.Context, taskID string, delay time.Duration) {
	r.mu.Lock()
	r.timersWG.Add(1)

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
			r.timersWG.Done()
		})

	if oldTimer, ok := r.delays[taskID]; ok {
		// Task ID collision should be extremely rare
		log.G(ctx).WithField("task.id", taskID).Error("start delay timer already exists for this task ID - replacing")
		if oldTimer.Stop() {
			r.timersWG.Done()
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
		r.timersWG.Done()
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
			r.timersWG.Done()
		}
	}
	r.delays = make(map[string]*time.Timer)
	r.history = make(map[instanceTuple]*instanceRestartInfo)
	r.mu.Unlock()
	r.timersWG.Wait()
}

// ClearServiceHistory forgets restart history related to a given service ID.
func (r *RestartSupervisor) ClearServiceHistory(serviceID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	tuples := r.historyByService[serviceID]
	if tuples == nil {
		return
	}

	delete(r.historyByService, serviceID)

	for t := range tuples {
		delete(r.history, t)
	}
}
