package exec

import (
	"fmt"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// ContainerController controls execution of container tasks.
type ContainerController interface {
	// ContainerStatus returns the status of the target container, if
	// available. When the container is not available, the status will be nil.
	ContainerStatus(ctx context.Context) (*api.ContainerStatus, error)
}

// Controller controls execution of a task.
type Controller interface {
	// Update the task definition seen by the controller. Will return
	// ErrTaskUpdateFailed if the provided task definition changes fields that
	// cannot be changed.
	//
	// Will be ignored if the task has exited.
	Update(ctx context.Context, t *api.Task) error

	// Prepare the task for execution. This should ensure that all resources
	// are created such that a call to start should execute immediately.
	Prepare(ctx context.Context) error

	// Start the target and return when it has started successfully.
	Start(ctx context.Context) error

	// Wait blocks until the target has exited.
	Wait(ctx context.Context) error

	// Shutdown requests to exit the target gracefully.
	Shutdown(ctx context.Context) error

	// Terminate the target.
	Terminate(ctx context.Context) error

	// Remove all resources allocated by the controller.
	Remove(ctx context.Context) error

	// Close closes any ephemeral resources associated with controller instance.
	Close() error
}

// Do progresses the task state using the controller performing a single
// operation on the controller. The return TaskStatus should be marked as the
// new state of the task.
//
// The returned status should be reported and placed back on to task
// before the next call. The operation can be cancelled by creating a
// cancelling context.
//
// Errors from the task controller will reported on the returned status. Any
// errors coming from this function should not be reported as related to the
// individual task.
//
// If ErrTaskNoop is returned, it means a second call to Do will result in no
// change. If ErrTaskDead is returned, calls to Do will no longer result in any
// action.
func Do(ctx context.Context, task *api.Task, ctlr Controller) (*api.TaskStatus, error) {
	status := task.Status.Copy()

	// stay in the current state.
	noop := func(errs ...error) (*api.TaskStatus, error) {
		return status, ErrTaskNoop
	}

	retry := func() (*api.TaskStatus, error) {
		// while we retry on all errors, this allows us to explicitly declare
		// retry cases.
		return status, ErrTaskRetry
	}

	// transition moves the task to the next state.
	transition := func(state api.TaskState, msg string) (*api.TaskStatus, error) {
		current := status.State
		status.State = state
		status.Message = msg

		if current > state {
			panic("invalid state transition")
		}
		return status, nil
	}

	// returned when a fatal execution of the task is fatal. In this case, we
	// proceed to a terminal error state and set the appropriate fields.
	//
	// Common checks for the nature of an error should be included here. If the
	// error is determined not to be fatal for the task,
	fatal := func(err error) (*api.TaskStatus, error) {
		log.G(ctx).WithError(err).Error("fatal task error")
		if err == nil {
			panic("err must not be nil when fatal")
		}

		if IsTemporary(err) {
			switch Cause(err) {
			case context.DeadlineExceeded, context.Canceled:
				// no need to set these errors, since these will more common.
			default:
				status.Err = err.Error()
			}

			return retry()
		}

		if cause := Cause(err); cause == context.DeadlineExceeded || cause == context.Canceled {
			return retry()
		}

		status.Err = err.Error()

		switch {
		case status.State < api.TaskStateStarting:
			status.State = api.TaskStateRejected
		case status.State > api.TaskStateStarting:
			status.State = api.TaskStateFailed
		}

		return status, nil
	}

	// below, we have several callbacks that are run after the state transition
	// is completed.

	defer func() {
		if task.Status.State != status.State {
			log.G(ctx).WithField("state.transition", fmt.Sprintf("%v->%v", task.Status.State, status.State)).
				Info("state changed")
		}
	}()

	// extract the container status from the container, if supported.
	defer func() {
		// only do this if in an active state
		if status.State < api.TaskStateStarting {
			return
		}

		cctlr, ok := ctlr.(ContainerController)
		if !ok {
			return
		}

		cstatus, err := cctlr.ContainerStatus(ctx)
		if err != nil {
			log.G(ctx).WithError(err).Error("container status unavailable")
			return
		}

		if cstatus != nil {
			status.RuntimeStatus = &api.TaskStatus_Container{
				Container: cstatus,
			}
		}
	}()

	switch task.DesiredState {
	case api.TaskStateNew, api.TaskStateAllocated,
		api.TaskStateAssigned, api.TaskStateAccepted,
		api.TaskStatePreparing, api.TaskStateReady,
		api.TaskStateStarting, api.TaskStateRunning,
		api.TaskStateCompleted, api.TaskStateFailed,
		api.TaskStateRejected:

		if task.DesiredState < status.State {
			// do not yet proceed. the desired state is less than the current
			// state.
			return noop()
		}

		switch status.State {
		case api.TaskStateNew, api.TaskStateAllocated,
			api.TaskStateAssigned:
			return transition(api.TaskStateAccepted, "accepted")
		case api.TaskStateAccepted:
			return transition(api.TaskStatePreparing, "preparing")
		case api.TaskStatePreparing:
			if err := ctlr.Prepare(ctx); err != nil && err != ErrTaskPrepared {
				return fatal(err)
			}

			return transition(api.TaskStateReady, "prepared")
		case api.TaskStateReady:
			return transition(api.TaskStateStarting, "starting")
		case api.TaskStateStarting:
			if err := ctlr.Start(ctx); err != nil && err != ErrTaskStarted {
				return fatal(err)
			}

			return transition(api.TaskStateRunning, "started")
		case api.TaskStateRunning:
			if err := ctlr.Wait(ctx); err != nil {
				// Wait should only proceed to failed if there is a terminal
				// error. The only two conditions when this happens are when we
				// get an exit code or when the container doesn't exist.
				switch err := err.(type) {
				case ExitCoder:
					return transition(api.TaskStateFailed, "failed")
				default:
					// pursuant to the above comment, report fatal, but wrap as
					// temporary.
					return fatal(MakeTemporary(err))
				}
			}

			return transition(api.TaskStateCompleted, "finished")
		default: // terminal states
			return noop()
		}
	case api.TaskStateShutdown:
		if status.State >= api.TaskStateShutdown {
			return noop()
		}

		if err := ctlr.Shutdown(ctx); err != nil {
			return fatal(err)
		}

		return transition(api.TaskStateShutdown, "shutdown")
	}

	panic("not reachable")
}
