package container

import (
	"net"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/docker/engine-api/types"
	enginecontainer "github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
)

const (
	// Explictly use the kernel's default setting for CPU quota of 100ms.
	// https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt
	cpuQuotaPeriod = 100 * time.Millisecond
)

// containerConfig converts task properties into docker container compatible
// components.
type containerConfig struct {
	task    *api.Task
	runtime *api.Container // resolved container specification.
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
	return strings.Join([]string{prefix, c.task.NodeID, c.task.ServiceID, c.task.ID}, ".")
}

func (c *containerConfig) image() string {
	return c.runtime.Image.Reference
}

func (c *containerConfig) config() *enginecontainer.Config {
	return &enginecontainer.Config{
		Cmd:        c.runtime.Command, // TODO(stevvooe): Fall back to entrypoint+args
		Env:        c.runtime.Env,
		WorkingDir: c.runtime.Dir,
		Image:      c.image(),
	}
}

func (c *containerConfig) hostConfig() *enginecontainer.HostConfig {
	return &enginecontainer.HostConfig{
		Resources: c.resources(),
	}
}

func (c *containerConfig) resources() enginecontainer.Resources {
	resources := enginecontainer.Resources{}

	// If no limits are specified let the engine use its defaults.
	//
	// TODO(aluzzardi): We might want to set some limits anyway otherwise
	// "unlimited" tasks will step over the reservation of other tasks.
	r := c.task.Spec.GetContainer().Resources
	if r == nil || r.Limits == nil {
		return resources
	}

	if r.Limits.MemoryBytes > 0 {
		resources.Memory = r.Limits.MemoryBytes
	}

	if r.Limits.NanoCPUs > 0 {
		// CPU Period must be set in microseconds.
		resources.CPUPeriod = int64(cpuQuotaPeriod / time.Microsecond)
		resources.CPUQuota = r.Limits.NanoCPUs * resources.CPUPeriod / 1e9
	}

	return resources
}

func (c *containerConfig) networkingConfig() *network.NetworkingConfig {
	epConfig := make(map[string]*network.EndpointSettings)
	for _, na := range c.task.Networks {
		var ipv4, ipv6 string
		for _, addr := range na.Addresses {
			ip, _, err := net.ParseCIDR(addr)
			if err != nil {
				continue
			}

			if ip.To4() != nil {
				ipv4 = ip.String()
				continue
			}

			if ip.To16() != nil {
				ipv6 = ip.String()
			}
		}

		epSettings := &network.EndpointSettings{
			IPAMConfig: &network.EndpointIPAMConfig{
				IPv4Address: ipv4,
				IPv6Address: ipv6,
			},
			ServiceConfig: &network.EndpointServiceConfig{
				Name: c.task.Meta.Name,
				ID:   c.task.ServiceID,
			},
		}

		epConfig[na.Network.Spec.Meta.Name] = epSettings
	}

	return &network.NetworkingConfig{EndpointsConfig: epConfig}
}

func (c *containerConfig) networkCreateOptions() []types.NetworkCreate {
	netOpts := make([]types.NetworkCreate, 0, len(c.task.Networks))
	for _, na := range c.task.Networks {
		options := types.NetworkCreate{
			Name:   na.Network.Spec.Meta.Name,
			ID:     na.Network.ID,
			Driver: na.Network.DriverState.Name,
			IPAM: network.IPAM{
				Driver: na.Network.IPAM.Driver.Name,
			},
			Options:        na.Network.DriverState.Options,
			CheckDuplicate: true,
		}

		for _, ic := range na.Network.IPAM.Configurations {
			c := network.IPAMConfig{
				Subnet:  ic.Subnet,
				IPRange: ic.Range,
				Gateway: ic.Gateway,
			}
			options.IPAM.Config = append(options.IPAM.Config, c)
		}

		netOpts = append(netOpts, options)

	}

	return netOpts
}

func (c *containerConfig) networks() []string {
	networks := make([]string, 0, len(c.task.Networks))
	for _, na := range c.task.Networks {
		networks = append(networks, na.Network.ID)
	}

	return networks
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
