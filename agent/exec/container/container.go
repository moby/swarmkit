package container

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/docker/distribution/reference"
	"github.com/docker/engine-api/types"
	enginecontainer "github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/go-connections/nat"
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
	task  *api.Task
	popts types.ImagePullOptions
}

// newContainerConfig returns a validated container config. No methods should
// return an error if this function returns without error.
func newContainerConfig(t *api.Task) (*containerConfig, error) {
	c := &containerConfig{task: t}

	container := t.GetContainer()
	if container == nil {
		return nil, exec.ErrRuntimeUnsupported
	}

	if container.Spec.Image.Reference == "" {
		return nil, ErrImageRequired
	}

	var err error
	c.popts, err = c.buildPullOptions()
	if err != nil {
		return nil, err
	}

	return c, nil
}

func (c *containerConfig) spec() *api.ContainerSpec {
	return &c.task.GetContainer().Spec
}

func (c *containerConfig) name() string {
	const prefix = "com.docker.cluster.task"
	return strings.Join([]string{prefix, c.task.NodeID, c.task.ServiceID, c.task.ID}, ".")
}

func (c *containerConfig) image() string {
	return c.spec().Image.Reference
}

func (c *containerConfig) ephemeralDirs() map[string]struct{} {
	mounts := c.spec().Mounts
	r := make(map[string]struct{})
	var x struct{}
	for _, val := range mounts {
		if val.Type == api.MountTypeEphemeral {
			r[val.Target] = x
		}
	}
	return r
}

func (c *containerConfig) config() *enginecontainer.Config {
	return &enginecontainer.Config{
		User:         c.spec().User,
		Cmd:          c.spec().Command, // TODO(stevvooe): Fall back to entrypoint+args
		Env:          c.spec().Env,
		WorkingDir:   c.spec().Dir,
		Image:        c.image(),
		ExposedPorts: c.exposedPorts(),
		Volumes:      c.ephemeralDirs(),
	}
}

func (c *containerConfig) exposedPorts() map[nat.Port]struct{} {
	exposedPorts := make(map[nat.Port]struct{})
	if c.spec().ExposedPorts != nil {
		for _, portConfig := range c.spec().ExposedPorts {
			port := nat.Port(fmt.Sprintf("%d/%s", portConfig.Port, strings.ToLower(portConfig.Protocol.String())))
			exposedPorts[port] = struct{}{}
		}
	}

	return exposedPorts
}

func (c *containerConfig) bindMounts() []string {
	mounts := c.spec().Mounts

	numBindMounts := 0
	for _, val := range mounts {
		if val.Type == api.MountTypeBind || val.Type == api.MountTypeVolume {
			numBindMounts++
		}
	}

	r := make([]string, numBindMounts)
	for i, val := range mounts {
		mask := ""
		switch val.Mask {
		case api.MountMaskReadOnly:
			mask = "ro"
		case api.MountMaskReadWrite:
			mask = "rw"
		}
		if val.Type == api.MountTypeBind {
			r[i] = fmt.Sprintf("%s:%s:%s", val.Source, val.Target, mask)
		} else if val.Type == api.MountTypeVolume {
			r[i] = fmt.Sprintf("%s:%s:%s", val.VolumeName, val.Target, mask)
		}
	}

	return r
}

func (c *containerConfig) hostConfig() *enginecontainer.HostConfig {
	return &enginecontainer.HostConfig{
		Resources:    c.resources(),
		Binds:        c.bindMounts(),
		PortBindings: c.portBindings(),
	}
}

func (c *containerConfig) portBindings() nat.PortMap {
	portBindings := nat.PortMap{}
	if c.spec().ExposedPorts != nil {
		for _, portConfig := range c.spec().ExposedPorts {
			port := nat.Port(fmt.Sprintf("%d/%s", portConfig.Port, strings.ToLower(portConfig.Protocol.String())))
			binding := []nat.PortBinding{
				{},
			}

			if portConfig.HostPort != 0 {
				binding[0].HostPort = strconv.Itoa(int(portConfig.HostPort))
			}
			portBindings[port] = binding
		}
	}

	return portBindings
}

func (c *containerConfig) resources() enginecontainer.Resources {
	resources := enginecontainer.Resources{}

	// If no limits are specified let the engine use its defaults.
	//
	// TODO(aluzzardi): We might want to set some limits anyway otherwise
	// "unlimited" tasks will step over the reservation of other tasks.
	r := c.spec().Resources
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
	var networks []*api.Container_NetworkAttachment
	if c.task.GetContainer() != nil {
		networks = c.task.GetContainer().Networks
	}

	epConfig := make(map[string]*network.EndpointSettings)
	for _, na := range networks {
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
				Name: c.task.Annotations.Name,
				ID:   c.task.ServiceID,
			},
		}

		epConfig[na.Network.Spec.Annotations.Name] = epSettings
	}

	return &network.NetworkingConfig{EndpointsConfig: epConfig}
}

func (c *containerConfig) networkCreateOptions() []types.NetworkCreate {
	if c.task.GetContainer() == nil {
		return nil
	}

	netOpts := make([]types.NetworkCreate, 0, len(c.task.GetContainer().Networks))
	for _, na := range c.task.GetContainer().Networks {
		options := types.NetworkCreate{
			Name:   na.Network.Spec.Annotations.Name,
			ID:     na.Network.ID,
			Driver: na.Network.DriverState.Name,
			IPAM: network.IPAM{
				Driver: na.Network.IPAM.Driver.Name,
			},
			Options:        na.Network.DriverState.Options,
			CheckDuplicate: true,
		}

		for _, ic := range na.Network.IPAM.Configs {
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
	if c.task.GetContainer() == nil {
		return nil
	}

	networks := make([]string, 0, len(c.task.GetContainer().Networks))
	for _, na := range c.task.GetContainer().Networks {
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
