package container

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sirupsen/logrus"
	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"golang.org/x/net/context"
)

// containerController conducts remote operations for a container. All calls
// are mostly naked calls to the client API, seeded with information from
// containerConfig.
type containerAdapter struct {
	client    engineapi.APIClient
	container *containerConfig
}

func newContainerAdapter(client engineapi.APIClient, task *api.Task) (*containerAdapter, error) {
	ctnr, err := newContainerConfig(task)
	if err != nil {
		return nil, err
	}

	return &containerAdapter{
		client:    client,
		container: ctnr,
	}, nil
}

func noopPrivilegeFn() (string, error) { return "", nil }

func (c *containerConfig) imagePullOptions() types.ImagePullOptions {
	var registryAuth string

	if c.spec().PullOptions != nil {
		registryAuth = c.spec().PullOptions.RegistryAuth
	}

	return types.ImagePullOptions{
		// if the image needs to be pulled, the auth config will be retrieved and updated
		RegistryAuth:  registryAuth,
		PrivilegeFunc: noopPrivilegeFn,
	}
}

func (c *containerAdapter) pullImage(ctx context.Context) error {
	rc, err := c.client.ImagePull(ctx, c.container.image(), c.container.imagePullOptions())
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

func (c *containerAdapter) createNetworks(ctx context.Context) error {
	for _, network := range c.container.networks() {
		opts, err := c.container.networkCreateOptions(network)
		if err != nil {
			return err
		}

		if _, err := c.client.NetworkCreate(ctx, network, opts); err != nil {
			if isNetworkExistError(err, network) {
				continue
			}

			return err
		}
	}

	return nil
}

func (c *containerAdapter) removeNetworks(ctx context.Context) error {
	for _, nid := range c.container.networks() {
		if err := c.client.NetworkRemove(ctx, nid); err != nil {
			if isActiveEndpointError(err) {
				continue
			}

			log.G(ctx).Errorf("network %s remove failed", nid)
			return err
		}
	}

	return nil
}

func (c *containerAdapter) create(ctx context.Context) error {
	if _, err := c.client.ContainerCreate(ctx,
		c.container.config(),
		c.container.hostConfig(),
		c.container.networkingConfig(),
		c.container.name()); err != nil {
		return err
	}

	return nil
}

func (c *containerAdapter) start(ctx context.Context) error {
	// TODO(nishanttotla): Consider adding checkpoint handling later
	return c.client.ContainerStart(ctx, c.container.name(), types.ContainerStartOptions{})
}

func (c *containerAdapter) inspect(ctx context.Context) (types.ContainerJSON, error) {
	return c.client.ContainerInspect(ctx, c.container.name())
}

// events issues a call to the events API and returns a channel with all
// events. The stream of events can be shutdown by cancelling the context.
//
// A chan struct{} is returned that will be closed if the event procressing
// fails and needs to be restarted.
func (c *containerAdapter) events(ctx context.Context) (<-chan events.Message, <-chan struct{}, error) {
	// TODO(stevvooe): Move this to a single, global event dispatch. For
	// now, we create a connection per container.
	var (
		eventsq = make(chan events.Message)
		closed  = make(chan struct{})
	)

	log.G(ctx).Debugf("waiting on events")
	// TODO(stevvooe): For long running tasks, it is likely that we will have
	// to restart this under failure.
	rc, err := c.client.Events(ctx, types.EventsOptions{
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

func (c *containerAdapter) shutdown(ctx context.Context) error {
	// Default stop grace period to 10s.
	stopgrace := 10 * time.Second
	spec := c.container.spec()
	if spec.StopGracePeriod != nil {
		stopgrace, _ = ptypes.Duration(spec.StopGracePeriod)
	}
	return c.client.ContainerStop(ctx, c.container.name(), stopgrace)
}

func (c *containerAdapter) terminate(ctx context.Context) error {
	return c.client.ContainerKill(ctx, c.container.name(), "")
}

func (c *containerAdapter) remove(ctx context.Context) error {
	return c.client.ContainerRemove(ctx, c.container.name(), types.ContainerRemoveOptions{
		RemoveVolumes: true,
		Force:         true,
	})
}

func (c *containerAdapter) createVolumes(ctx context.Context, client engineapi.APIClient) error {
	// Create plugin volumes that are embedded inside a Mount
	for _, mount := range c.container.spec().Mounts {
		if mount.Type != api.MountTypeVolume {
			continue
		}

		// we create volumes when there is a volume driver available volume options
		if mount.VolumeOptions == nil {
			continue
		}

		if mount.VolumeOptions.DriverConfig == nil {
			continue
		}

		req := c.container.volumeCreateRequest(&mount)
		if _, err := client.VolumeCreate(ctx, *req); err != nil {
			// TODO(amitshukla): Today, volume create through the engine api does not return an error
			// when the named volume with the same parameters already exists.
			// It returns an error if the driver name is different - that is a valid error
			return err
		}
	}

	return nil
}

// TODO(mrjana/stevvooe): There is no proper error code for network not found
// error in engine-api. Resort to string matching until engine-api is fixed.

func isActiveEndpointError(err error) bool {
	return strings.Contains(err.Error(), "has active endpoints")
}

func isNetworkExistError(err error, name string) bool {
	return strings.Contains(err.Error(), fmt.Sprintf("network with name %s already exists", name))
}

func isContainerCreateNameConflict(err error) bool {
	return strings.Contains(err.Error(), "Conflict. The name")
}

func isUnknownContainer(err error) bool {
	return strings.Contains(err.Error(), "No such container:")
}

func isStoppedContainer(err error) bool {
	return strings.Contains(err.Error(), "is already stopped")
}
