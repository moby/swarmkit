package restart

import (
	"container/list"
	"errors"
	"math/rand"
	"sync"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/defaults"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
	"golang.org/x/net/context"
)

const defaultOldTaskTimeout = time.Minute

type restartedInstance struct {
	timestamp time.Time
}

// instanceRestartInfo represents the restart history of a
// single running task.
type instanceRestartInfo struct {
	// Counter of restarts for this instance.  Used with Restart.MaxAttempts
	// to determine if a task should be restarted.
	totalRestarts uint64
	// Counter of failures since the last call of Success.  Used with
	// Restart.Backoff to calculate the exponential backoff if applicable.
	failuresSinceSuccess uint64
	// Timestamp of the last restart.  Used with Restart.Backoff.Monitor
	// to determine when to reset failuresSinceSuccess.
	lastRestart time.Time
	// Linked list of restartedInstance structs. Only used when
	// Restart.MaxAttempts and Restart.Window are both
	// nonzero.
	restartedInstances *list.List
	// Why is specVersion in this structure and not in the map key? While
	// putting it in the key would be a very simple solution, it wouldn't
	// be easy to clean up map entries corresponding to old specVersions.
	// Making the key version-agnostic and clearing the value whenever the
	// version changes avoids the issue of stale map entries for old
	// versions.
	specVersion api.Version
}

type delayedStart struct {
	// cancel is called to cancel the delayed start.
	cancel func()
	doneCh chan struct{}

	// waiter is set to true if the next restart is waiting for this delay
	// to complete.
	waiter bool
}

// Supervisor initiates and manages restarts. It's responsible for
// delaying restarts when applicable.
type Supervisor struct {
	mu               sync.Mutex
	store            *store.MemoryStore
	delays           map[string]*delayedStart
	historyByService map[string]map[orchestrator.SlotTuple]*instanceRestartInfo
	TaskTimeout      time.Duration
}

// NewSupervisor creates a new RestartSupervisor.
func NewSupervisor(store *store.MemoryStore) *Supervisor {
	return &Supervisor{
		store:            store,
		delays:           make(map[string]*delayedStart),
		historyByService: make(map[string]map[orchestrator.SlotTuple]*instanceRestartInfo),
		TaskTimeout:      defaultOldTaskTimeout,
	}
}

func (r *Supervisor) waitRestart(ctx context.Context, oldDelay *delayedStart, cluster *api.Cluster, taskID string) {
	// Wait for the last restart delay to elapse.
	select {
	case <-oldDelay.doneCh:
	case <-ctx.Done():
		return
	}

	// Start the next restart
	err := r.store.Update(func(tx store.Tx) error {
		t := store.GetTask(tx, taskID)
		if t == nil {
			return nil
		}
		if t.DesiredState > api.TaskStateRunning {
			return nil
		}
		service := store.GetService(tx, t.ServiceID)
		if service == nil {
			return nil
		}
		return r.Restart(ctx, tx, cluster, service, *t)
	})

	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to restart task after waiting for previous restart")
	}
}

// Restart initiates a new task to replace t if appropriate under the service's
// restart policy.
func (r *Supervisor) Restart(ctx context.Context, tx store.Tx, cluster *api.Cluster, service *api.Service, t api.Task) error {
	// TODO(aluzzardi): This function should not depend on `service`.

	// Is the old task still in the process of restarting? If so, wait for
	// its restart delay to elapse, to avoid tight restart loops (for
	// example, when the image doesn't exist).
	r.mu.Lock()
	oldDelay, ok := r.delays[t.ID]
	if ok {
		if !oldDelay.waiter {
			oldDelay.waiter = true
			go r.waitRestart(ctx, oldDelay, cluster, t.ID)
		}
		r.mu.Unlock()
		return nil
	}
	r.mu.Unlock()

	// Sanity check: was the task shut down already by a separate call to
	// Restart? If so, we must avoid restarting it, because this will create
	// an extra task. This should never happen unless there is a bug.
	if t.DesiredState > api.TaskStateRunning {
		return errors.New("Restart called on task that was already shut down")
	}

	t.DesiredState = api.TaskStateShutdown
	err := store.UpdateTask(tx, &t)
	if err != nil {
		log.G(ctx).WithError(err).Errorf("failed to set task desired state to dead")
		return err
	}

	if !r.shouldRestart(ctx, &t, service) {
		return nil
	}

	var restartTask *api.Task

	if orchestrator.IsReplicatedService(service) {
		restartTask = orchestrator.NewTask(cluster, service, t.Slot, "")
	} else if orchestrator.IsGlobalService(service) {
		restartTask = orchestrator.NewTask(cluster, service, 0, t.NodeID)
	} else {
		log.G(ctx).Error("service not supported by restart supervisor")
		return nil
	}

	n := store.GetNode(tx, t.NodeID)

	restartTask.DesiredState = api.TaskStateReady

	var restartDelay time.Duration
	// Restart delay is not applied to drained nodes
	if n == nil || n.Spec.Availability != api.NodeAvailabilityDrain {
		r.checkForSuccess(&t)
		if ret, randomize, err := r.TaskRestartDelay(ctx, &t); err == nil {
			// Choose a uniformly distributed value from [0, backoffDuration).
			if randomize && ret > 0 {
				restartDelay = time.Duration(rand.Int63n(int64(ret)))
			} else {
				restartDelay = ret
			}
		} else {
			return err
		}
	}

	waitStop := true

	// Normally we wait for the old task to stop running, but we skip this
	// if the old task is already dead or the node it's assigned to is down.
	if (n != nil && n.Status.State == api.NodeStatus_DOWN) || t.Status.State > api.TaskStateRunning {
		waitStop = false
	}

	if err := store.CreateTask(tx, restartTask); err != nil {
		log.G(ctx).WithError(err).WithField("task.id", restartTask.ID).Error("task create failed")
		return err
	}

	tuple := orchestrator.SlotTuple{
		Slot:      restartTask.Slot,
		ServiceID: restartTask.ServiceID,
		NodeID:    restartTask.NodeID,
	}
	r.RecordRestartHistory(tuple, restartTask)

	r.DelayStart(ctx, tx, &t, restartTask.ID, restartDelay, waitStop)
	return nil
}

// TaskRestartDelay calculates and returns the next delay based on t's RestartPolicy.
// randomize is true if a backoff policy is being used,  false if the RestartPolicy
// is configured for a fixed restart delay
func (r *Supervisor) TaskRestartDelay(ctx context.Context, t *api.Task) (restartDelay time.Duration,
	randomize bool, parseError error) {
	restartDelay = 0
	backoff := &api.BackoffPolicy{
		Factor: defaults.Service.Task.Restart.Backoff.Factor,
		Max:    defaults.Service.Task.Restart.Backoff.Max,
	}
	// If no restart is defined we use the default exponential backoff configuration
	if t.Spec.Restart != nil {
		var delayErr error
		if delay, delayErr := gogotypes.DurationFromProto(t.Spec.Restart.Delay); delayErr == nil {
			restartDelay = delay
		}
		if t.Spec.Restart.Backoff == nil && t.Spec.Restart.Delay != nil {
			// Use fixed restart delay instead of exponential
			if delayErr != nil {
				log.G(ctx).WithError(delayErr).Error("invalid restart delay; using default")
				// We only use this default if the user has configured for fixed restart delay
				restartDelay, delayErr = gogotypes.DurationFromProto(defaults.Service.Task.Restart.Delay)

				// If user defined a delay without a backoff, but that delay and the
				// default delay can't be read we fail fast instead of defaulting to
				// a randomized exponential backoff.
				if delayErr != nil {
					return 0, false, errors.New("error parsing RestartPolicy")
				}
			}
			return restartDelay, false, nil
		}
		// If the user defined backoff can be read we override default values
		if _, err := gogotypes.DurationFromProto(t.Spec.Restart.Backoff.Factor); err == nil {
			backoff.Factor = t.Spec.Restart.Backoff.Factor
		}
		if _, err := gogotypes.DurationFromProto(t.Spec.Restart.Backoff.Max); err == nil {
			backoff.Max = t.Spec.Restart.Backoff.Max
		}
	}

	// Use jittered binary exponential backoff delay
	var factor, max time.Duration
	var err error
	if factor, err = gogotypes.DurationFromProto(backoff.Factor); err != nil {
		return 0, false, errors.New("error parsing backoff Factor")
	}
	if max, err = gogotypes.DurationFromProto(backoff.Max); err != nil {
		return 0, false, errors.New("error parsing backoff Max")
	}

	// Get the failure count from history
	var failures uint64
	r.mu.Lock()
	defer r.mu.Unlock()
	serviceID := t.ServiceID
	tuple := orchestrator.SlotTuple{
		Slot:      t.Slot,
		ServiceID: t.ServiceID,
		NodeID:    t.NodeID,
	}
	if r.historyByService[serviceID] != nil &&
		r.historyByService[serviceID][tuple] != nil {
		restartInfo := r.historyByService[serviceID][tuple]
		failures = restartInfo.failuresSinceSuccess
	} else {
		// History is not set on the first run of a task
		failures = 0
	}

	var backoffDuration time.Duration
	// Use Restart.Delay on first run or when the factor is 0.
	if failures == 0 || factor == 0 {
		backoffDuration = restartDelay
		// Check for values greater than Restart.Max  We use failures-1 so that on the first failure
		// delay will be restartDelay+factor on second failure +2*factor on third +4*factor etc.
		// Note: A 1 nanosecond factor with 62 failures would give over a 146 year restart delay.
	} else if failures < 63 && (max-restartDelay)/factor > 1<<(failures-1) {
		backoffDuration = restartDelay + factor*time.Duration(1<<(failures-1))
	} else {
		backoffDuration = max
	}

	return backoffDuration, true, nil
}

// shouldRestart returns true if a task should be restarted according to the
// restart policy.
func (r *Supervisor) shouldRestart(ctx context.Context, t *api.Task, service *api.Service) bool {
	// TODO(aluzzardi): This function should not depend on `service`.
	condition := orchestrator.RestartCondition(t)

	if condition != api.RestartOnAny &&
		(condition != api.RestartOnFailure || t.Status.State == api.TaskStateCompleted) {
		return false
	}

	if t.Spec.Restart == nil || t.Spec.Restart.MaxAttempts == 0 {
		return true
	}

	instanceTuple := orchestrator.SlotTuple{
		Slot:      t.Slot,
		ServiceID: t.ServiceID,
	}

	// Slot is not meaningful for "global" tasks, so they need to be
	// indexed by NodeID.
	if orchestrator.IsGlobalService(service) {
		instanceTuple.NodeID = t.NodeID
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	restartInfo := r.historyByService[t.ServiceID][instanceTuple]
	if restartInfo == nil || (t.SpecVersion != nil && *t.SpecVersion != restartInfo.specVersion) {
		return true
	}

	if t.Spec.Restart.Window == nil || (t.Spec.Restart.Window.Seconds == 0 && t.Spec.Restart.Window.Nanos == 0) {
		return restartInfo.totalRestarts < t.Spec.Restart.MaxAttempts
	}

	if restartInfo.restartedInstances == nil {
		return true
	}

	window, err := gogotypes.DurationFromProto(t.Spec.Restart.Window)
	if err != nil {
		log.G(ctx).WithError(err).Error("invalid restart lookback window")
		return restartInfo.totalRestarts < t.Spec.Restart.MaxAttempts
	}

	var timestamp time.Time
	// Prefer the manager's timestamp over the agent's, since manager
	// clocks are more trustworthy.
	if t.Status.AppliedAt != nil {
		timestamp, err = gogotypes.TimestampFromProto(t.Status.AppliedAt)
		if err != nil {
			log.G(ctx).WithError(err).Error("invalid task status AppliedAt timestamp")
			return restartInfo.totalRestarts < t.Spec.Restart.MaxAttempts
		}
	} else {
		// It's safe to call TimestampFromProto with a nil timestamp
		timestamp, err = gogotypes.TimestampFromProto(t.Status.Timestamp)
		if t.Status.Timestamp == nil || err != nil {
			log.G(ctx).WithError(err).Error("invalid task completion timestamp")
			return restartInfo.totalRestarts < t.Spec.Restart.MaxAttempts
		}
	}
	lookback := timestamp.Add(-window)

	numRestarts := uint64(restartInfo.restartedInstances.Len())

	// Disregard any restarts that happened before the lookback window,
	// and remove them from the linked list since they will no longer
	// be relevant to figuring out if tasks should be restarted going
	// forward.
	var next *list.Element
	for e := restartInfo.restartedInstances.Front(); e != nil; e = next {
		next = e.Next()

		if e.Value.(restartedInstance).timestamp.After(lookback) {
			break
		}
		restartInfo.restartedInstances.Remove(e)
		numRestarts--
	}

	// Ignore restarts that didn't happen before the task we're looking at.
	for e2 := restartInfo.restartedInstances.Back(); e2 != nil; e2 = e2.Prev() {
		if e2.Value.(restartedInstance).timestamp.Before(timestamp) {
			break
		}
		numRestarts--
	}

	if restartInfo.restartedInstances.Len() == 0 {
		restartInfo.restartedInstances = nil
	}

	return numRestarts < t.Spec.Restart.MaxAttempts
}

// UpdatableTasksInSlot returns the set of tasks that should be passed to the
// updater from this slot, or an empty slice if none should be.  An updatable
// slot has either at least one task that with desired state <= RUNNING, or its
// most recent task has stopped running and should not be restarted. The latter
// case is for making sure that tasks that shouldn't normally be restarted will
// still be handled by rolling updates when they become outdated.  There is a
// special case for rollbacks to make sure that a rollback always takes the
// service to a converged state, instead of ignoring tasks with the original
// spec that stopped running and shouldn't be restarted according to the
// restart policy.
func (r *Supervisor) UpdatableTasksInSlot(ctx context.Context, slot orchestrator.Slot, service *api.Service) orchestrator.Slot {
	if len(slot) < 1 {
		return nil
	}

	var updatable orchestrator.Slot
	for _, t := range slot {
		if t.DesiredState <= api.TaskStateRunning {
			updatable = append(updatable, t)
		}
	}
	if len(updatable) > 0 {
		return updatable
	}

	if service.UpdateStatus != nil && service.UpdateStatus.State == api.UpdateStatus_ROLLBACK_STARTED {
		return nil
	}

	// Find most recent task
	byTimestamp := orchestrator.TasksByTimestamp(slot)
	newestIndex := 0
	for i := 1; i != len(slot); i++ {
		if byTimestamp.Less(newestIndex, i) {
			newestIndex = i
		}
	}

	if !r.shouldRestart(ctx, slot[newestIndex], service) {
		return orchestrator.Slot{slot[newestIndex]}
	}
	return nil
}

// RecordRestartHistory updates the historyByService map to reflect the restart
// of restartedTask.
func (r *Supervisor) RecordRestartHistory(tuple orchestrator.SlotTuple, replacementTask *api.Task) {
	if spec := replacementTask.Spec.Restart; spec != nil && spec.Backoff == nil &&
		spec.Delay != nil && spec.MaxAttempts == 0 {
		// No need to record history if we are using fixed restart delay
		// with unlimited attempts.
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	serviceID := replacementTask.ServiceID
	if r.historyByService[serviceID] == nil {
		r.historyByService[serviceID] = make(map[orchestrator.SlotTuple]*instanceRestartInfo)
	}
	if r.historyByService[serviceID][tuple] == nil {
		r.historyByService[serviceID][tuple] = &instanceRestartInfo{}
	}

	restartInfo := r.historyByService[serviceID][tuple]

	if replacementTask.SpecVersion != nil && *replacementTask.SpecVersion != restartInfo.specVersion {
		// This task has a different SpecVersion from the one we're
		// tracking. Most likely, the service was updated. Past failures
		// shouldn't count against the new service definition, so clear
		// the history for this instance.
		*restartInfo = instanceRestartInfo{
			specVersion: *replacementTask.SpecVersion,
		}
	}

	restartInfo.totalRestarts++
	restartInfo.failuresSinceSuccess++

	// it's okay to call TimestampFromProto with a nil argument
	timestamp, err := gogotypes.TimestampFromProto(replacementTask.Meta.CreatedAt)
	if replacementTask.Meta.CreatedAt == nil || err != nil {
		timestamp = time.Now()
	}
	restartInfo.lastRestart = timestamp

	if replacementTask.Spec.Restart == nil || replacementTask.Spec.Restart.MaxAttempts == 0 {
		return
	}

	if replacementTask.Spec.Restart.Window != nil && (replacementTask.Spec.Restart.Window.Seconds != 0 || replacementTask.Spec.Restart.Window.Nanos != 0) {
		if restartInfo.restartedInstances == nil {
			restartInfo.restartedInstances = list.New()
		}

		restartedInstance := restartedInstance{
			timestamp: timestamp,
		}

		restartInfo.restartedInstances.PushBack(restartedInstance)
	}
}

// checkForSuccess resets the failuresSinceSuccess count if the duration since
// the last restart is more than the monitoring period
func (r *Supervisor) checkForSuccess(task *api.Task) {
	r.mu.Lock()
	defer r.mu.Unlock()

	monitor, err := gogotypes.DurationFromProto(defaults.Service.Task.Restart.Backoff.Monitor)
	if err != nil {
		return
	}
	if task.Spec.Restart != nil && task.Spec.Restart.Backoff != nil &&
		task.Spec.Restart.Backoff.Monitor != nil {
		if m, err := gogotypes.DurationFromProto(task.Spec.Restart.Backoff.Monitor); err != nil {
			monitor = m
		}
	}
	serviceID := task.ServiceID
	tuple := orchestrator.SlotTuple{
		Slot:      task.Slot,
		ServiceID: task.ServiceID,
		NodeID:    task.NodeID,
	}
	if r.historyByService[serviceID] != nil &&
		r.historyByService[serviceID][tuple] != nil {
		restartInfo := r.historyByService[serviceID][tuple]

		if time.Since(restartInfo.lastRestart) > monitor {
			restartInfo.failuresSinceSuccess = 0
		}
	}
}

// GetFailuresSinceSuccess returns failuresSinceSuccess of the given task
func (r *Supervisor) GetFailuresSinceSuccess(task *api.Task) uint64 {
	r.mu.Lock()
	defer r.mu.Unlock()

	serviceID := task.ServiceID
	tuple := orchestrator.SlotTuple{
		Slot:      task.Slot,
		ServiceID: task.ServiceID,
		NodeID:    task.NodeID,
	}

	if r.historyByService[serviceID] != nil &&
		r.historyByService[serviceID][tuple] != nil {
		restartInfo := r.historyByService[serviceID][tuple]

		return restartInfo.failuresSinceSuccess
	}
	// restartInfo.failuresSinceSuccess should be zero on first run of task
	return 0
}

// DelayStart starts a timer that moves the task from READY to RUNNING once:
// - The restart delay has elapsed (if applicable)
// - The old task that it's replacing has stopped running (or this times out)
// It must be called during an Update transaction to ensure that it does not
// miss events. The purpose of the store.Tx argument is to avoid accidental
// calls outside an Update transaction.
func (r *Supervisor) DelayStart(ctx context.Context, _ store.Tx, oldTask *api.Task, newTaskID string, delay time.Duration, waitStop bool) <-chan struct{} {
	ctx, cancel := context.WithCancel(context.Background())
	doneCh := make(chan struct{})

	r.mu.Lock()
	for {
		oldDelay, ok := r.delays[newTaskID]
		if !ok {
			break
		}
		oldDelay.cancel()
		r.mu.Unlock()
		// Note that this channel read should only block for a very
		// short time, because we cancelled the existing delay and
		// that should cause it to stop immediately.
		<-oldDelay.doneCh
		r.mu.Lock()
	}
	r.delays[newTaskID] = &delayedStart{cancel: cancel, doneCh: doneCh}
	r.mu.Unlock()

	var watch chan events.Event
	cancelWatch := func() {}

	waitForTask := waitStop && oldTask != nil && oldTask.Status.State <= api.TaskStateRunning

	if waitForTask {
		// Wait for either the old task to complete, or the old task's
		// node to become unavailable.
		watch, cancelWatch = state.Watch(
			r.store.WatchQueue(),
			api.EventUpdateTask{
				Task:   &api.Task{ID: oldTask.ID, Status: api.TaskStatus{State: api.TaskStateRunning}},
				Checks: []api.TaskCheckFunc{api.TaskCheckID, state.TaskCheckStateGreaterThan},
			},
			api.EventUpdateNode{
				Node:   &api.Node{ID: oldTask.NodeID, Status: api.NodeStatus{State: api.NodeStatus_DOWN}},
				Checks: []api.NodeCheckFunc{api.NodeCheckID, state.NodeCheckState},
			},
			api.EventDeleteNode{
				Node:   &api.Node{ID: oldTask.NodeID},
				Checks: []api.NodeCheckFunc{api.NodeCheckID},
			},
		)
	}

	go func() {
		defer func() {
			cancelWatch()
			r.mu.Lock()
			delete(r.delays, newTaskID)
			r.mu.Unlock()
			close(doneCh)
		}()

		oldTaskTimer := time.NewTimer(r.TaskTimeout)
		defer oldTaskTimer.Stop()

		// Wait for the delay to elapse, if one is specified.
		if delay != 0 {
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return
			}
		}

		if waitForTask {
			select {
			case <-watch:
			case <-oldTaskTimer.C:
			case <-ctx.Done():
				return
			}
		}

		err := r.store.Update(func(tx store.Tx) error {
			err := r.StartNow(tx, newTaskID)
			if err != nil {
				log.G(ctx).WithError(err).WithField("task.id", newTaskID).Error("moving task out of delayed state failed")
			}
			return nil
		})
		if err != nil {
			log.G(ctx).WithError(err).WithField("task.id", newTaskID).Error("task restart transaction failed")
		}
	}()

	return doneCh
}

// StartNow moves the task into the RUNNING state so it will proceed to start
// up.
func (r *Supervisor) StartNow(tx store.Tx, taskID string) error {
	t := store.GetTask(tx, taskID)
	if t == nil || t.DesiredState >= api.TaskStateRunning {
		return nil
	}
	t.DesiredState = api.TaskStateRunning
	return store.UpdateTask(tx, t)
}

// Cancel cancels a pending restart.
func (r *Supervisor) Cancel(taskID string) {
	r.mu.Lock()
	delay, ok := r.delays[taskID]
	r.mu.Unlock()

	if !ok {
		return
	}

	delay.cancel()
	<-delay.doneCh
}

// CancelAll aborts all pending restarts and waits for any instances of
// StartNow that have already triggered to complete.
func (r *Supervisor) CancelAll() {
	var cancelled []delayedStart

	r.mu.Lock()
	for _, delay := range r.delays {
		delay.cancel()
	}
	r.mu.Unlock()

	for _, delay := range cancelled {
		<-delay.doneCh
	}
}

// ClearServiceHistory forgets restart history related to a given service ID.
func (r *Supervisor) ClearServiceHistory(serviceID string) {
	r.mu.Lock()
	delete(r.historyByService, serviceID)
	r.mu.Unlock()
}
