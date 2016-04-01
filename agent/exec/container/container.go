package container

import (
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/docker/engine-api/types"
	enginecontainer "github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
)

// containerConfig converts task properties into docker container compatible
// components.
type containerConfig struct {
	task    *api.Task
	runtime *api.ContainerSpec // resolved container specification.
	popts   types.ImagePullOptions
}

// newContainerConfig returns a validated container config. No methods should
// return an error if this function returns without error.
func newContainerConfig(t *api.Task) (*containerConfig, error) {
	c := &containerConfig{task: t}

	runtime := t.Spec.GetContainer()
	if runtime == nil {
		return nil, exec.ErrRuntimeUnsupported
	}

	if runtime.Image == nil {
		return nil, ErrImageRequired
	}

	if runtime.Image.Reference == "" {
		return nil, ErrImageRequired
	}

	c.runtime = runtime

	var err error
	c.popts, err = c.buildPullOptions()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *containerConfig) name() string {
	const prefix = "com.docker.cluster.task"
	return strings.Join([]string{prefix, c.task.NodeID, c.task.JobID, c.task.ID}, ".")
}

func (c *containerConfig) image() string {
	return c.runtime.Image.Reference
}

func (c *containerConfig) config() *enginecontainer.Config {
	return &enginecontainer.Config{
		Cmd:   c.runtime.Command, // TODO(stevvooe): Fall back to entrypoint+args
		Env:   c.runtime.Env,
		Image: c.image(),
	}
}

func (c *containerConfig) hostConfig() *enginecontainer.HostConfig {
	return &enginecontainer.HostConfig{}
}

func (c *containerConfig) networkingConfig() *network.NetworkingConfig {
	return &network.NetworkingConfig{}
}

func (c *containerConfig) pullOptions() types.ImagePullOptions {
	return c.popts
}

func (c *containerConfig) buildPullOptions() (types.ImagePullOptions, error) {
	named, err := reference.ParseNamed(c.image())
	if err != nil {
		return types.ImagePullOptions{}, err
	}

	var (
		name = named.Name()
		tag  = "latest"
	)

	// replace tag with more specific item from ref
	switch v := named.(type) {
	case reference.Canonical:
		tag = v.Digest().String()
	case reference.NamedTagged:
		tag = v.Tag()

	}

	return types.ImagePullOptions{
		ImageID: name,
		Tag:     tag,
	}, nil
}

func (c containerConfig) eventFilter() filters.Args {
	filter := filters.NewArgs()
	filter.Add("type", events.ContainerEventType)
	filter.Add("name", c.name())
	return filter
}
