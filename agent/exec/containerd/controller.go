package containerd

import (
	"fmt"

	"github.com/containerd/containerd/api/types/task"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

type controller struct {
	task    *api.Task
	adapter *containerAdapter
	closed  chan struct{}
	err     error

	pulled     chan struct{} // closed after pull
	cancelPull func()        // cancels pull context if not nil
	pullErr    error         // pull error, protected by close of pulled
}

var _ exec.Controller = &controller{}

func newController(conn *grpc.ClientConn, containerDir string, task *api.Task, secrets exec.SecretGetter) (exec.Controller, error) {
	adapter, err := newContainerAdapter(conn, containerDir, task, secrets)
	if err != nil {
		return nil, err
	}

	return &controller{
		task:    task,
		adapter: adapter,
		closed:  make(chan struct{}),
	}, nil
}

// Update takes a recent task update and applies it to the container.
func (r *controller) Update(ctx context.Context, t *api.Task) error {
	log.G(ctx).Warnf("task updates not yet supported")
	// TODO(stevvooe): While assignment of tasks is idempotent, we do allow
	// updates of metadata, such as labelling, as well as any other properties
	// that make sense.
	return nil
}

// Prepare creates a container and ensures the image is pulled.
//
// If the container has already be created, exec.ErrTaskPrepared is returned.
func (r *controller) Prepare(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	//// Make sure all the networks that the task needs are created.
	// TODO(ijc)
	//if err := r.adapter.createNetworks(ctx); err != nil {
	//	return err
	//}

	//// Make sure all the volumes that the task needs are created.
	// TODO(ijc)
	//if err := r.adapter.createVolumes(ctx); err != nil {
	//	return err
	//}

	if r.pulled == nil {
		// Launches a re-entrant pull operation associated with controller,
		// dissociating the context from the caller's context. Allows pull
		// operation to be re-entrant on calls to prepare, resuming from the
		// same point after cancellation.
		var pctx context.Context

		r.pulled = make(chan struct{})
		pctx, r.cancelPull = context.WithCancel(context.Background()) // TODO(stevvooe): Bind a context to the entire controller.

		go func() {
			defer close(r.pulled)
			r.pullErr = r.adapter.pullImage(pctx)
		}()
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.pulled:
		if r.pullErr != nil {
			// NOTE(stevvooe): We always try to pull the image to make sure we have
			// the most up to date version. This will return an error, but we only
			// log it. If the image truly doesn't exist, the create below will
			// error out.
			//
			// This gives us some nice behavior where we use up to date versions of
			// mutable tags, but will still run if the old image is available but a
			// registry is down.
			//
			// If you don't want this behavior, lock down your image to an
			// immutable tag or digest.
			log.G(ctx).WithError(r.pullErr).Error("pulling image failed")
		}
	}

	if err := r.adapter.create(ctx); err != nil {
		if isContainerCreateNameConflict(err) {
			if _, err := r.adapter.inspect(ctx); err != nil {
				return err
			}

			// container is already created. success!
			return exec.ErrTaskPrepared
		}

		return errors.Wrap(err, "create container failed")
	}

	return nil
}

// Start the container. An error will be returned if the container is already started.
func (r *controller) Start(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	ctnr, err := r.adapter.inspect(ctx)
	if err != nil {
		return err
	}

	// Detect whether the container has *ever* been started. If so, we don't
	// issue the start.
	//
	// TODO(stevvooe): This is very racy. While reading inspect, another could
	// start the process and we could end up starting it twice.
	if ctnr.Status != task.StatusCreated {
		return exec.ErrTaskStarted
	}

	if err := r.adapter.start(ctx); err != nil {
		return errors.Wrap(err, "starting container failed")
	}

	// TODO(ijc): Wait for HealtCheck to report OK.

	return nil
}

// Wait on the container to exit.
func (r *controller) Wait(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	// check the initial state and report that.
	ctnr, err := r.adapter.inspect(ctx)
	if err != nil {
		return errors.Wrap(err, "inspecting container failed")
	}

	shutdownWithExitStatus := func(reason string) error {
		exitStatus, err := r.adapter.shutdown(ctx)
		if err != nil {
			return err
		}
		log.G(ctx).Errorf("EXIT STATUS %v", exitStatus)
		return makeExitError(exitStatus, reason)
	}
	switch ctnr.Status {
	case task.StatusStopped:
		return shutdownWithExitStatus("")
	}

	// We do not disable FailFast for this initial call (like we
	// do on the retry below) since we are still halfway through
	// setting up the container and if containerd goes away half
	// way through we consider that a failure.
	eventq, closed, err := r.adapter.events(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-eventq:
			log.G(ctx).Debugf("Event: %v", event)

			switch event.Type {
			case task.Event_EXIT:
				return shutdownWithExitStatus("")
			case task.Event_OOM:
				return shutdownWithExitStatus("Container OOMd")
			case task.Event_CREATE, task.Event_START, task.Event_EXEC_ADDED, task.Event_PAUSED:
				continue
			default:
				return errors.Errorf("Unknown event type %s\n", event.Type.String())
			}
		case <-closed:
			// restart!
			log.G(ctx).Debugf("Restarting event stream")
			// We disable FailFast for this call so that gRPC will keep
			// retrying while we wait for containerd to come back. Otherwise
			// a temporary glitch in the connection (e.g. a containerd restart)
			// will result in the task being declared dead even though it is
			// likely to be recoverable.
			eventq, closed, err = r.adapter.events(ctx, grpc.FailFast(false))
			if err != nil {
				return err
			}

			// recheck the container state, if this fails then we may have missed a
			ctnr, err := r.adapter.inspect(ctx)
			if err != nil {
				return errors.Wrap(err, "inspecting container on event restart failed")
			}
			switch ctnr.Status {
			case task.StatusStopped:
				return shutdownWithExitStatus("container had exited after event stream restart")
			}

		case <-ctx.Done():
			return ctx.Err()
		case <-r.closed:
			return r.err
		}
	}
}

// Shutdown the container cleanly.
func (r *controller) Shutdown(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	if r.cancelPull != nil {
		r.cancelPull()
	}

	if _, err := r.adapter.shutdown(ctx); err != nil {
		if isUnknownContainer(err) {
			return nil
		}

		return err
	}

	return nil
}

// Terminate the container, with force.
func (r *controller) Terminate(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	if r.cancelPull != nil {
		r.cancelPull()
	}

	if err := r.adapter.terminate(ctx); err != nil {
		if isUnknownContainer(err) {
			return nil
		}

		return err
	}

	return nil
}

// Remove the container and its resources.
func (r *controller) Remove(ctx context.Context) error {
	ctx = log.WithModule(ctx, "containerd")

	if err := r.checkClosed(); err != nil {
		return err
	}

	if r.cancelPull != nil {
		r.cancelPull()
	}

	// It may be necessary to shut down the task before removing it.
	if err := r.Shutdown(ctx); err != nil {
		if isUnknownContainer(err) {
			return nil
		}

		// This may fail if the task was already shut down.
		log.G(ctx).WithError(err).Debug("shutdown failed on removal")
	}

	// Try removing networks referenced in this task in case this
	// task is the last one referencing it
	// TODO(ijc)
	//if err := r.adapter.removeNetworks(ctx); err != nil {
	//	if isUnknownContainer(err) {
	//		return nil
	//	}

	//	return err
	//}

	if err := r.adapter.remove(ctx); err != nil {
		if isUnknownContainer(err) {
			return nil
		}

		return err
	}

	return nil
}

// Close the controller and clean up any ephemeral resources.
func (r *controller) Close() error {
	select {
	case <-r.closed:
		return r.err
	default:
		if r.cancelPull != nil {
			r.cancelPull()
		}

		r.err = exec.ErrControllerClosed
		close(r.closed)
	}
	return nil
}

func (r *controller) checkClosed() error {
	select {
	case <-r.closed:
		return r.err
	default:
		return nil
	}
}

type exitError struct {
	code  uint32
	cause error
}

func (e *exitError) Error() string {
	if e.cause != nil {
		return fmt.Sprintf("task: non-zero exit (%v): %v", e.code, e.cause)
	}
	return fmt.Sprintf("task: non-zero exit (%v)", e.code)
}

func (e *exitError) ExitCode() int {
	return int(e.code)
}

func (e *exitError) Cause() error {
	return e.cause
}

func makeExitError(exitStatus uint32, reason string) error {
	if exitStatus != 0 {
		var cause error
		if reason != "" {
			cause = errors.New(reason)
		}

		return &exitError{
			code:  exitStatus,
			cause: cause,
		}
	}

	return nil
}
