package exec

import (
	"github.com/docker/swarm-v2/log"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"golang.org/x/net/context"
)

// Runner controls execution of a task.
//
// All methods should be idempotent and thread-safe.
type Runner interface {
	// Update the task definition seen by the runner. Will return
	// ErrTaskUpdateFailed if the provided task definition changes fields that
	// cannot be changed.
	//
	// Will be ignored if the task has exited.
	Update(ctx context.Context, t *objectspb.Task) error

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

	// Remove all resources allocated by the runner.
	Remove(ctx context.Context) error

	// Close closes any ephemeral resources associated with runner instance.
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
	Report(ctx context.Context, state typespb.TaskState) error

	// TODO(stevvooe): It is very likely we will need to report more
	// information back from the runner into the agent. We'll likely expand
	// this interface to do so.
}

// Run runs a runner, reporting state along the way. Under normal execution,
// this function blocks until the task is completed.
func Run(ctx context.Context, runner Runner, reporter Reporter) error {
	if err := report(ctx, reporter, typespb.TaskStatePreparing); err != nil {
		return err
	}

	if err := runner.Prepare(ctx); err != nil {
		switch err {
		case ErrTaskPrepared:
			log.G(ctx).Warnf("already prepared")
			return runStart(ctx, runner, reporter)
		case ErrTaskStarted:
			log.G(ctx).Warnf("already started")
			return runWait(ctx, runner, reporter)
		default:
			return err
		}
	}

	if err := report(ctx, reporter, typespb.TaskStateReady); err != nil {
		return err
	}

	return runStart(ctx, runner, reporter)
}

// runStart reports that the task is starting, calls Start and hands execution
// off to `runWait`. It will block until task execution is completed or an
// error is encountered.
func runStart(ctx context.Context, runner Runner, reporter Reporter) error {
	if err := report(ctx, reporter, typespb.TaskStateStarting); err != nil {
		return err
	}

	if err := runner.Start(ctx); err != nil {
		switch err {
		case ErrTaskStarted:
			log.G(ctx).Warnf("already started")
		default:
			return err
		}
	}
	return runWait(ctx, runner, reporter)
}

// runWait reports that the task is running and calls Wait. When Wait exits,
// the task will be reported as completed.
func runWait(ctx context.Context, runner Runner, reporter Reporter) error {
	if err := report(ctx, reporter, typespb.TaskStateRunning); err != nil {
		return err
	}

	if err := runner.Wait(ctx); err != nil {
		// NOTE(stevvooe): We *do not* handle the exit error here,
		// since we may do something different based on whether we
		// are in SHUTDOWN or having an unplanned exit,
		return err
	}

	return report(ctx, reporter, typespb.TaskStateCompleted)
}

func report(ctx context.Context, reporter Reporter, state typespb.TaskState) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	log.G(ctx).WithField("state", state).Debugf("Report")
	return reporter.Report(ctx, state)
}
