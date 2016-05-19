package container

import (
	"errors"
	"strings"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// controller implements agent.Controller against docker's API.
//
// Most operations against docker's API are done through the container name,
// which is unique to the task.
type controller struct {
	client  engineapi.APIClient
	task    *api.Task
	adapter *containerAdapter
	closed  chan struct{}
	err     error
}

var _ exec.Controller = &controller{}

// newController returns a dockerexec controller for the provided task.
func newController(client engineapi.APIClient, task *api.Task) (exec.Controller, error) {
	adapter, err := newContainerAdapter(client, task)
	if err != nil {
		return nil, err
	}

	return &controller{
		client:  client,
		task:    task,
		adapter: adapter,
		closed:  make(chan struct{}),
	}, nil
}

func (r *controller) Task() (*api.Task, error) {
	return r.task, nil
}

// ContainerStatus returns the container-specific status for the task.
func (r *controller) ContainerStatus(ctx context.Context) (*api.ContainerStatus, error) {
	ctnr, err := r.adapter.inspect(ctx)
	if err != nil {
		return nil, err
	}
	return parseContainerStatus(ctnr)
}

// Update tasks a recent task update and applies it to the container.
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
	if err := r.checkClosed(); err != nil {
		return err
	}

	// Make sure all the networks that the task needs are created.
	if err := r.adapter.createNetworks(ctx); err != nil {
		return err
	}

	// Make sure all the volumes that the task needs are created.
	if err := r.adapter.createVolumes(ctx, r.client); err != nil {
		return err
	}

	for {
		if err := r.checkClosed(); err != nil {
			return err
		}

		if err := r.adapter.create(ctx); err != nil {
			if isContainerCreateNameConflict(err) {
				if _, err := r.adapter.inspect(ctx); err != nil {
					return err
				}

				// container is already created. success!
				return exec.ErrTaskPrepared
			}

			if !engineapi.IsErrImageNotFound(err) {
				return err
			}

			if err := r.adapter.pullImage(ctx); err != nil {
				return err
			}

			continue // retry to create the container
		}

		break
	}

	return nil
}

func isContainerCreateNameConflict(err error) bool {
	// TODO(stevvooe): Very fragile error reporting from daemon. Need better
	// errors in engineapi.
	return strings.Contains(err.Error(), "Conflict. The name")
}

// Start the container. An error will be returned if the container is already started.
func (r *controller) Start(ctx context.Context) error {
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
	if ctnr.State.Status != "created" {
		return exec.ErrTaskStarted
	}

	if err := r.adapter.start(ctx); err != nil {
		return err
	}

	return nil
}

// Wait on the container to exit.
func (r *controller) Wait(pctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	// check the initial state and report that.
	ctnr, err := r.adapter.inspect(ctx)
	if err != nil {
		return err
	}

	switch ctnr.State.Status {
	case "exited", "dead":
		// TODO(stevvooe): Treating container status dead as exited. There may
		// be more to do if we have dead containers. Note that this is not the
		// same as task state DEAD, which means the container is completely
		// freed on a node.

		return makeExitError(ctnr)
	}

	eventq, closed, err := r.adapter.events(ctx)
	if err != nil {
		return err
	}

	for {
		select {
		case event := <-eventq:
			if !r.matchevent(event) {
				continue
			}

			switch event.Action {
			case "die": // exit on terminal events
				ctnr, err := r.adapter.inspect(ctx)
				if err != nil {
					return err
				}

				return makeExitError(ctnr)
			case "destroy":
				// If we get here, something has gone wrong but we want to exit
				// and report anyways.
				return ErrContainerDestroyed
			}
		case <-closed:
			// restart!
			eventq, closed, err = r.adapter.events(ctx)
			if err != nil {
				return err
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
	if err := r.checkClosed(); err != nil {
		return err
	}

	return r.adapter.shutdown(ctx)
}

// Terminate the container, with force.
func (r *controller) Terminate(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	return r.adapter.terminate(ctx)
}

// Remove the container and its resources.
func (r *controller) Remove(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	// It may be necessary to shut down the task before removing it.
	if err := r.Shutdown(ctx); err != nil {
		// This may fail if the task was already shut down.
		log.G(ctx).WithError(err).Debug("shutdown failed on removal")
	}

	// Try removing networks referenced in this task in case this
	// task is the last one referencing it
	if err := r.adapter.removeNetworks(ctx); err != nil {
		return err
	}

	return r.adapter.remove(ctx)
}

// Close the controller and clean up any ephemeral resources.
func (r *controller) Close() error {
	select {
	case <-r.closed:
		return r.err
	default:
		r.err = exec.ErrControllerClosed
		close(r.closed)
	}
	return nil
}

func (r *controller) matchevent(event events.Message) bool {
	if event.Type != events.ContainerEventType {
		return false
	}

	// TODO(stevvooe): Filter based on ID matching, in addition to name.

	// Make sure the events are for this container.
	if event.Actor.Attributes["name"] != r.adapter.container.name() {
		return false
	}

	return true
}

func (r *controller) checkClosed() error {
	select {
	case <-r.closed:
		return r.err
	default:
		return nil
	}
}

func makeExitError(ctnr types.ContainerJSON) error {
	if ctnr.State.ExitCode != 0 {
		var cause error
		if ctnr.State.Error != "" {
			cause = errors.New(ctnr.State.Error)
		}

		cstatus, _ := parseContainerStatus(ctnr)
		return &exec.ExitError{
			Code:            ctnr.State.ExitCode,
			Cause:           cause,
			ContainerStatus: cstatus,
		}
	}

	return nil

}

func parseContainerStatus(ctnr types.ContainerJSON) (*api.ContainerStatus, error) {
	status := &api.ContainerStatus{
		ContainerID: ctnr.ID,
		PID:         int32(ctnr.State.Pid),
		ExitCode:    int32(ctnr.State.ExitCode),
	}

	return status, nil
}
