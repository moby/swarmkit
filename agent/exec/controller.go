package exec

import (
	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// Controller controls execution of a task.
//
// All methods should be idempotent and thread-safe.
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

// Reporter defines an interface for calling back into the task status
// reporting infrastructure. Typically, an instance is associated to a specific
// task.
//
// The results of the "Report" are combined with a TaskStatus and sent to the
// dispatcher.
type Reporter interface {
	// Report the state of the task run. If an error is returned, execution
	// will be stopped.
	Report(ctx context.Context, state api.TaskState, msg string) error

	// TODO(stevvooe): It is very likely we will need to report more
	// information back from the controller into the agent. We'll likely expand
	// this interface to do so.
}

// Run runs a controller, reporting state along the way. Under normal execution,
// this function blocks until the task is completed.
func Run(ctx context.Context, ctlr Controller, reporter Reporter) error {
	if err := report(ctx, reporter, api.TaskStatePreparing, "preparing"); err != nil {
		return err
	}

	if err := ctlr.Prepare(ctx); err != nil {
		switch err {
		case ErrTaskPrepared:
			log.G(ctx).Warnf("already prepared")
			return runStart(ctx, ctlr, reporter, "already prepared")
		case ErrTaskStarted:
			log.G(ctx).Warnf("already started")
			return runWait(ctx, ctlr, reporter, "already started")
		default:
			return err
		}
	}

	if err := report(ctx, reporter, api.TaskStateReady, "prepared"); err != nil {
		return err
	}

	return runStart(ctx, ctlr, reporter, "starting")
}

// Shutdown the task using the controller and report on the status.
func Shutdown(ctx context.Context, ctlr Controller, reporter Reporter) error {
	if err := ctlr.Shutdown(ctx); err != nil {
		return err
	}

	return report(ctx, reporter, api.TaskStateShutdown, "shutdown requested")
}

// Remove the task for the controller and report on the status.
func Remove(ctx context.Context, ctlr Controller, reporter Reporter) error {
	if err := report(ctx, reporter, api.TaskStateFinalize, "removing"); err != nil {
		return err
	}

	if err := ctlr.Remove(ctx); err != nil {
		log.G(ctx).WithError(err).Error("remove failed")
		if err := report(ctx, reporter, api.TaskStateFinalize, "remove failed"); err != nil {
			log.G(ctx).WithError(err).Error("report remove error failed")
			return err
		}
	}

	return report(ctx, reporter, api.TaskStateDead, "finalized")
}

// runStart reports that the task is starting, calls Start and hands execution
// off to `runWait`. It will block until task execution is completed or an
// error is encountered.
func runStart(ctx context.Context, ctlr Controller, reporter Reporter, msg string) error {
	if err := report(ctx, reporter, api.TaskStateStarting, msg); err != nil {
		return err
	}

	msg = "started"
	if err := ctlr.Start(ctx); err != nil {
		switch err {
		case ErrTaskStarted:
			log.G(ctx).Warnf("already started")
			msg = "already started"
		default:
			return err
		}
	}

	return runWait(ctx, ctlr, reporter, msg)
}

// runWait reports that the task is running and calls Wait. When Wait exits,
// the task will be reported as completed.
func runWait(ctx context.Context, ctlr Controller, reporter Reporter, msg string) error {
	if err := report(ctx, reporter, api.TaskStateRunning, msg); err != nil {
		return err
	}

	if err := ctlr.Wait(ctx); err != nil {
		// NOTE(stevvooe): We *do not* handle the exit error here,
		// since we may do something different based on whether we
		// are in SHUTDOWN or having an unplanned exit,
		return err
	}

	return report(ctx, reporter, api.TaskStateCompleted, "completed")
}

func report(ctx context.Context, reporter Reporter, state api.TaskState, msg string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	ctx = log.WithLogger(ctx, log.G(ctx).WithFields(
		logrus.Fields{
			"state":      state,
			"status.msg": msg}))
	log.G(ctx).Debug("report status")
	return reporter.Report(ctx, state, msg)
}
