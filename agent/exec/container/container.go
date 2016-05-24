package container

import (
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

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
	task *api.Task

	networksAttachments map[string]*api.Container_NetworkAttachment
}

// newContainerConfig returns a validated container config. No methods should
// return an error if this function returns without error.
func newContainerConfig(t *api.Task) (*containerConfig, error) {
	var c containerConfig
	return &c, c.setTask(t)
}

func (c *containerConfig) setTask(t *api.Task) error {
	container := t.GetContainer()
	if container == nil {
		return exec.ErrRuntimeUnsupported
	}

	if container.Spec.Image.Reference == "" {
		return ErrImageRequired
	}

	// index the networks by name
	c.networksAttachments = make(map[string]*api.Container_NetworkAttachment, len(container.Networks))
	for _, attachment := range container.Networks {
		c.networksAttachments[attachment.Network.Spec.Annotations.Name] = attachment
	}

	c.task = t
	return nil
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
	var r []string

	for _, val := range c.spec().Mounts {
		mask := ""
		switch val.Mask {
		case api.MountMaskReadOnly:
			mask = "ro"
		case api.MountMaskReadWrite:
			mask = "rw"
		}
		if val.Type == api.MountTypeBind {
			r = append(r, fmt.Sprintf("%s:%s:%s", val.Source, val.Target, mask))
		} else if val.Type == api.MountTypeVolume {
			r = append(r, fmt.Sprintf("%s:%s:%s", val.VolumeName, val.Target, mask))
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

func (c *containerConfig) volumeCreateRequest(vol *api.Volume) *types.VolumeCreateRequest {
	return &types.VolumeCreateRequest{
		Name:       vol.Spec.Annotations.Name,
		Driver:     vol.Spec.DriverConfiguration.Name,
		DriverOpts: optsAsMap(vol.Spec.DriverConfiguration.Options),
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

// networks returns a list of network names attached to the container. The
// returned name can be used to lookup the corresponding network create
// options.
func (c *containerConfig) networks() []string {
	var networks []string

	for name := range c.networksAttachments {
		networks = append(networks, name)
	}

	return networks
}

func (c *containerConfig) networkCreateOptions(name string) (types.NetworkCreate, error) {
	na, ok := c.networksAttachments[name]
	if !ok {
		return types.NetworkCreate{}, errors.New("container: unknown network referenced")
	}

	options := types.NetworkCreate{
		ID:     na.Network.ID,
		Driver: na.Network.DriverState.Name,
		IPAM: network.IPAM{
			Driver: na.Network.IPAM.Driver.Name,
		},
		Options:        optsAsMap(na.Network.DriverState.Options),
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

	return options, nil
}

func (c containerConfig) eventFilter() filters.Args {
	filter := filters.NewArgs()
	filter.Add("type", events.ContainerEventType)
	filter.Add("name", c.name())
	return filter
}

// optsAsMap converts key-value '='-separated to a map. Duplicates take the
// latest value.
func optsAsMap(opts []string) map[string]string {
	m := make(map[string]string, len(opts))
	for _, opt := range opts {
		parts := strings.SplitN(opt, "=", 2)

		if len(parts) > 1 {
			m[parts[0]] = parts[1]
		} else {
			m[parts[0]] = ""
		}
	}

	return m
}
