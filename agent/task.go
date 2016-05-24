package agent

import (
	"reflect"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// StatusReporter receives updates to task status. Method may be called
// concurrently, so implementations should be goroutine-safe.
type StatusReporter interface {
	UpdateTaskStatus(ctx context.Context, taskID string, status *api.TaskStatus) error
}

// taskManager manages all aspects of task execution and reporting for an agent
// through state management.
type taskManager struct {
	ctlr     exec.Controller
	reporter StatusReporter
	requestq chan modificationRequest
	wg       sync.WaitGroup

	// protected defines fields that should only be access in run loop or a modify function.
	protected struct {
		task *api.Task
	}

	removed chan struct{}
	closed  chan struct{}
}

func newTaskManager(ctx context.Context, task *api.Task, ctlr exec.Controller, reporter StatusReporter) *taskManager {
	t := &taskManager{
		ctlr:     ctlr,
		reporter: reporter,
		requestq: make(chan modificationRequest),
		removed:  make(chan struct{}),
		closed:   make(chan struct{}),
	}
	t.protected.task = task
	go t.run(ctx)
	return t
}

// Update the task data. Blocks until the update is accepted.
func (tm *taskManager) Update(ctx context.Context, task *api.Task) error {
	return modify(ctx, tm.requestq, tm.closed, ErrClosed, func(ctx context.Context) (deferred func(), err error) {
		if tasksEqual(tm.protected.task, task) {
			return nil, nil // ignore the update
		}

		if task.ID != tm.protected.task.ID {
			return nil, errTaskInvalid
		}

		task = task.Copy()
		task.Status = tm.protected.task.Status // overwrite our status, as it is canonical.

		// protect against reversing desired state
		if tm.protected.task.DesiredState > task.DesiredState {
			task.DesiredState = tm.protected.task.DesiredState
		}

		tm.protected.task = task

		return func() {
			// propagate the update to the controller, synchronously.
			if err := tm.ctlr.Update(ctx, task); err != nil {
				log.G(ctx).WithError(err).Error("failed task update")
			}
		}, nil
	})
}

// Remove marks the task manager to remove the task resources.
//
// Does not block on operation. May be called multiple task is fully removed.
// Once this function returns nil, the task has been completely removed.
func (tm *taskManager) Remove(ctx context.Context) error {
	log.G(ctx).Debug("(*taskManager).Remove")
	return modify(ctx, tm.requestq, tm.removed, nil, func(ctx context.Context) (func(), error) {
		log.G(ctx).Debug("(*taskManager).Remove", "modify")
		if tm.protected.task.DesiredState != api.TaskStateDead {
			tm.protected.task.DesiredState = api.TaskStateDead
		}

		return nil, ErrRemoving
	})
}

func (tm *taskManager) Close() error {
	log.L.Debug("(*taskManager).Close")
	select {
	case <-tm.closed:
		return ErrClosed
	default:
		close(tm.closed)
		return nil
	}
}

// dispatch does an operation on for the task. Typically, this called one at a
// time by setting taskManager.protected.op.
//
// Only call this from within a protected section.
func (tm *taskManager) dispatch(ctx context.Context) {
	task := tm.protected.task.Copy()
	tm.wg.Add(1)
	go func() {
		status, err := exec.Do(ctx, task, tm.ctlr)
		if err != nil {
			switch err {
			case exec.ErrTaskNoop:
			case exec.ErrTaskDead:
				// if we made a transition from remove to dead, we never
				// want to call Do again. We close the remove channel to
				// signal this condition.
				close(tm.removed)
			default:
				log.G(ctx).WithError(err).Error("exec.Do failed")
			}
		}
		tm.wg.Done()

		log.G(ctx).WithError(err).WithField("task.status.next", status).Debug("exec.Do succeeded")
		if err := modify(ctx, tm.requestq, tm.closed, ErrClosed, func(ctx context.Context) (deferred func(), err error) {
			tm.protected.task.Status = *status
			return nil, err // propagates error from exec.Do
		}); err != nil {
			if err != ErrClosed && err != context.Canceled && err != context.DeadlineExceeded {
				log.G(ctx).WithError(err).Error("failed to update task status")
			}
		}
	}()
}

func (tm *taskManager) run(ctx context.Context) {
	defer func() {
		if ctx.Err() != nil {
			log.G(ctx).WithError(ctx.Err()).Error("task manager exiting")
		} else {
			log.G(ctx).Debug("task manager exiting")
		}
	}()

	// pump the requestq.
	go func() {
		if err := modify(ctx, tm.requestq, tm.closed, ErrClosed, func(ctx context.Context) (func(), error) {
			return nil, nil
		}); err != nil {
			log.G(ctx).WithError(err).Error("failed to start taskManager loop")
			return
		}
	}()

	opctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel() // closure let's us pick up current value of cancel.
	}()

	status := tm.protected.task.Status // keep track of last sent status
	for {
		if status != tm.protected.task.Status {
			// always try to report status on each cycle.
			if err := tm.reporter.UpdateTaskStatus(ctx, tm.protected.task.ID, tm.protected.task.Status.Copy()); err != nil {
				log.G(ctx).WithError(err).Error("failed reporting status to agent")
				continue // keep retrying the report.
			}

			status = tm.protected.task.Status
		}

		select {
		case request := <-tm.requestq:
			deferred, err := request.fn(ctx)
			request.response <- err

			// always cancel outstanding operation
			cancel()

			if deferred != nil {
				deferred()
			}

			// before doing anything else, we wait on the cancelled operation to return.
			tm.wg.Wait()

			// after a request, we always dispatch, unless we have ErrTaskNoop.
			if err != exec.ErrTaskNoop && err != exec.ErrTaskDead {
				opctx, cancel = context.WithCancel(ctx)
				opctx = log.WithLogger(opctx, log.G(ctx).WithFields(
					logrus.Fields{
						"task.state":         tm.protected.task.Status.State,
						"task.state.desired": tm.protected.task.DesiredState,
					}))

				tm.dispatch(opctx)
			} else {
				log.G(ctx).Debug("suspending task after noop")
			}
		case <-tm.closed:
			return
		case <-ctx.Done():
			return
		}
	}
}

// tasksEqual returns true if the tasks are functionaly equal, ignoring status,
// version and other superfluous fields.
//
// This used to decide whether or not to propagate a task update to a controller.
func tasksEqual(a, b *api.Task) bool {
	a, b = a.Copy(), b.Copy()

	a.Status, b.Status = api.TaskStatus{}, api.TaskStatus{}
	a.Meta, b.Meta = api.Meta{}, api.Meta{}

	return reflect.DeepEqual(a, b)
}
