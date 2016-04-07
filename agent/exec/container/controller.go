package container

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/Sirupsen/logrus"
	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
)

// containerController conducts remote operations for a container. All calls
// are mostly naked calls to the client API, seeded with information from
// containerConfig.
type containerController struct {
	container *containerConfig
}

func newContainerController(task *api.Task) (*containerController, error) {
	ctnr, err := newContainerConfig(task)
	if err != nil {
		return nil, err
	}

	return &containerController{
		container: ctnr,
	}, nil
}

func noopPrivilegeFn() (string, error) { return "", nil }

func (c *containerController) pullImage(ctx context.Context, client engineapi.APIClient) error {

	rc, err := client.ImagePull(ctx, c.container.pullOptions(), noopPrivilegeFn)
	if err != nil {
		return err
	}

	dec := json.NewDecoder(rc)
	m := map[string]interface{}{}
	for {
		if err := dec.Decode(&m); err != nil {
			if err == io.EOF {
				break
			}
			return err
		}
		// TOOD(stevvooe): Report this status somewhere.
		logrus.Debugln("pull progress", m)
	}
	// if the final stream object contained an error, return it
	if errMsg, ok := m["error"]; ok {
		return fmt.Errorf("%v", errMsg)
	}
	return nil
}

func (c *containerController) create(ctx context.Context, client engineapi.APIClient) error {
	if _, err := client.ContainerCreate(ctx,
		c.container.config(),
		c.container.hostConfig(),
		c.container.networkingConfig(),
		c.container.name()); err != nil {
		return err
	}

	return nil
}

func (c *containerController) start(ctx context.Context, client engineapi.APIClient) error {
	return client.ContainerStart(ctx, c.container.name())
}

func (c *containerController) inspect(ctx context.Context, client engineapi.APIClient) (types.ContainerJSON, error) {
	return client.ContainerInspect(ctx, c.container.name())
}

// events issues a call to the events API and returns a channel with all
// events. The stream of events can be shutdown by cancelling the context.
//
// A chan struct{} is returned that will be closed if the event procressing
// fails and needs to be restarted.
func (c *containerController) events(ctx context.Context, client engineapi.APIClient) (<-chan events.Message, <-chan struct{}, error) {
	// TODO(stevvooe): Move this to a single, global event dispatch. For
	// now, we create a connection per container.
	var (
		eventsq = make(chan events.Message)
		closed  = make(chan struct{})
	)

	log.G(ctx).Debugf("waiting on events")
	// TODO(stevvooe): For long running tasks, it is likely that we will have
	// to restart this under failure.
	rc, err := client.Events(ctx, types.EventsOptions{
		Since:   "0",
		Filters: c.container.eventFilter(),
	})
	if err != nil {
		return nil, nil, err
	}

	go func(rc io.ReadCloser) {
		defer rc.Close()
		defer close(closed)

		select {
		case <-ctx.Done():
			// exit
			return
		default:
		}

		dec := json.NewDecoder(rc)

		for {
			var event events.Message
			if err := dec.Decode(&event); err != nil {
				// TODO(stevvooe): This error handling isn't quite right.
				if err == io.EOF {
					return
				}

				log.G(ctx).Errorf("error decoding event: %v", err)
				return
			}

			select {
			case eventsq <- event:
			case <-ctx.Done():
				return
			}
		}
	}(rc)

	return eventsq, closed, nil
}

func (c *containerController) shutdown(ctx context.Context, client engineapi.APIClient) error {
	timeout, err := resolveTimeout(ctx)
	if err != nil {
		return err
	}

	// TODO(stevvooe): Sending Stop isn't quite right. The timeout is actually
	// a grace period between SIGTERM and SIGKILL. We'll have to play with this
	// a little but to figure how much we defer to the engine.
	return client.ContainerStop(ctx, c.container.name(), timeout)
}

func (c *containerController) terminate(ctx context.Context, client engineapi.APIClient) error {
	return client.ContainerKill(ctx, c.container.name(), "")
}

func (c *containerController) remove(ctx context.Context, client engineapi.APIClient) error {
	return client.ContainerRemove(ctx, types.ContainerRemoveOptions{
		ContainerID:   c.container.name(),
		RemoveVolumes: true,
		Force:         true,
	})
}

// resolveTimeout calculates the timeout for second granularity timeout using
// the context's deadline.
func resolveTimeout(ctx context.Context) (int, error) {
	timeout := 10 // we need to figure out how to pick this value.
	if deadline, ok := ctx.Deadline(); ok {
		left := deadline.Sub(time.Now())

		if left <= 0 {
			<-ctx.Done()
			return 0, ctx.Err()
		}

		timeout = int(left.Seconds())
	}
	return timeout, nil
}
