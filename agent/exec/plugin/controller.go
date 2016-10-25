package plugin

import (
	engineapi "github.com/docker/docker/client"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/pkg/errors"
	"golang.org/x/net/context"
)

// controller implements agent.Controller against docker's API.
//
// Most operations against docker's API are done through the plugin name,
// which is unique to the task.
type controller struct {
	task    *api.Task
	adapter *pluginAdapter
	closed  chan struct{}
	err     error
}

var _ exec.Controller = &controller{}

// NewController returns a docker exec controller for the provided task.
func NewController(client engineapi.APIClient, task *api.Task) (exec.Controller, error) {
	adapter, err := newPluginAdapter(client, task)
	if err != nil {
		return nil, err
	}

	return &controller{
		task:    task,
		adapter: adapter,
		closed:  make(chan struct{}),
	}, nil
}

func (r *controller) Task() *api.Task {
	return r.task
}

// Update tasks a recent task update and applies it to the plugin.
func (r *controller) Update(ctx context.Context, t *api.Task) error {
	log.G(ctx).Warnf("task updates not yet supported")
	// TODO(stevvooe): While assignment of tasks is idempotent, we do allow
	// updates of metadata, such as labelling, as well as any other properties
	// that make sense.
	return nil
}

// Prepare create a plugin and ensures the image is pulled.
func (r *controller) Prepare(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	if err := r.adapter.install(ctx); err != nil {
		if isPluginExists(err) {
			if _, err := r.adapter.inspect(ctx); err != nil {
				return err
			}

			// plugin is already installed. success!
			return exec.ErrTaskPrepared
		}

		return err
	}

	return nil
}

// Enable the plugin.
func (r *controller) Start(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	if !r.adapter.plugin.disabled() {
		plgn, err := r.adapter.inspect(ctx)
		if err != nil {
			return err
		}

		if plgn.Enabled {
			return exec.ErrTaskStarted
		}

		if err := r.adapter.enable(ctx); err != nil {
			return errors.Wrap(err, "enabling plugin failed")
		}
	}

	return nil
}

// Wait forever.
func (r *controller) Wait(pctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(pctx)
	defer cancel()

	// check the initial state and report that.
	if _, err := r.adapter.inspect(ctx); err != nil {
		return errors.Wrap(err, "inspecting plugin failed")
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-r.closed:
		return r.err
	}
}

// Shutdown is currently the same as Terminate.
// TODO: Shutdown the plugin cleanly once in possible in docker/docker.
func (r *controller) Shutdown(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	if err := r.adapter.disable(ctx); err != nil {
		if isPluginAlreadyDisabled(err) {
			return nil
		}
		return err
	}

	return nil
}

// Terminate the plugin.
func (r *controller) Terminate(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	if err := r.adapter.disable(ctx); err != nil {
		if isPluginAlreadyDisabled(err) {
			return nil
		}
		return err
	}

	return nil
}

// Remove the plugin.
func (r *controller) Remove(ctx context.Context) error {
	if err := r.checkClosed(); err != nil {
		return err
	}

	// It may be necessary to shut down the task before removing it.
	if err := r.Shutdown(ctx); err != nil {
		// This may fail if the task was already shut down.
		log.G(ctx).WithError(err).Debug("shutdown failed on removal")
	}

	if err := r.adapter.remove(ctx); err != nil {
		return err
	}

	return nil
}

// Close the controller.
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

func (r *controller) checkClosed() error {
	select {
	case <-r.closed:
		return r.err
	default:
		return nil
	}
}
