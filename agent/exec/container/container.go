package container

import (
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/docker/engine-api/types"
	enginecontainer "github.com/docker/engine-api/types/container"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/engine-api/types/filters"
	"github.com/docker/engine-api/types/network"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/spec"
)

const (
	// Explictly use the kernel's default setting for CPU quota of 100ms.
	// https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt
	cpuQuotaPeriod = 100 * time.Millisecond

	// systemLabelPrefix represents the reserved namespace for system labels.
	systemLabelPrefix = "com.docker.swarm"
)

// containerConfig converts task properties into docker container compatible
// components.
type containerConfig struct {
	task *api.Task

	networksAttachments map[string]*api.NetworkAttachment
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

	if container.Spec.Image == "" {
		return ErrImageRequired
	}

	// index the networks by name
	c.networksAttachments = make(map[string]*api.NetworkAttachment, len(t.Networks))
	for _, attachment := range t.Networks {
		c.networksAttachments[attachment.Network.Spec.Annotations.Name] = attachment
	}

	c.task = t
	return nil
}

func (c *containerConfig) endpoint() *api.Endpoint {
	return c.task.Endpoint
}

func (c *containerConfig) spec() *api.ContainerSpec {
	return &c.task.GetContainer().Spec
}

func (c *containerConfig) name() string {
	if container := c.task.GetContainer(); container != nil && container.Annotations.Name != "" {
		// if set, use the container Annotations.Name field, set in the orchestrator.
		return container.Annotations.Name
	}

	// fallback to service.instance.id.
	return strings.Join([]string{c.task.ServiceAnnotations.Name, fmt.Sprint(c.task.Instance), c.task.ID}, ".")
}

func (c *containerConfig) image() string {
	return c.spec().Image
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
	config := &enginecontainer.Config{
		Labels:     c.labels(),
		User:       c.spec().User,
		Env:        c.spec().Env,
		WorkingDir: c.spec().Dir,
		Image:      c.image(),
		Volumes:    c.ephemeralDirs(),
	}

	if len(c.spec().Command) > 0 {
		// If Command is provided, we replace the whole invocation with Command
		// by replacing Entrypoint and specifying Cmd. Args is ignored in this
		// case.
		config.Entrypoint = append(config.Entrypoint, c.spec().Command[0])
		config.Cmd = append(config.Cmd, c.spec().Command[1:]...)
	} else if len(c.spec().Args) > 0 {
		// In this case, we assume the image has an Entrypoint and Args
		// specifies the arguments for that entrypoint.
		config.Cmd = c.spec().Args
	}

	return config
}

func (c *containerConfig) labels() map[string]string {
	var (
		system = map[string]string{
			"task":         "", // mark as cluster task
			"task.id":      c.task.ID,
			"task.name":    fmt.Sprintf("%v.%v", c.task.ServiceAnnotations.Name, c.task.Instance),
			"node.id":      c.task.NodeID,
			"service.id":   c.task.ServiceID,
			"service.name": c.task.ServiceAnnotations.Name,
		}
		labels = make(map[string]string)
	)

	// base labels are those defined in the spec.
	for k, v := range c.spec().Labels {
		labels[k] = v
	}

	if container := c.task.GetContainer(); container != nil {
		// we then apply the overrides from container, which may be set via the
		// orchestrator.
		for k, v := range container.Annotations.Labels {
			labels[k] = v
		}
	}

	// finally, we apply the system labels, which override all labels.
	for k, v := range system {
		labels[strings.Join([]string{systemLabelPrefix, k}, ".")] = v
	}

	return labels
}

func (c *containerConfig) bindMounts() []string {
	var r []string

	for _, val := range c.spec().Mounts {
		mask := getMountMask(val)
		if val.Type == api.MountTypeBind {
			r = append(r, fmt.Sprintf("%s:%s:%s", val.Source, val.Target, mask))
		} else if val.Type == api.MountTypeVolume {
			r = append(r, fmt.Sprintf("%s:%s:%s", val.VolumeName, val.Target, mask))
		}
	}

	return r
}

func getMountMask(m *api.Mount) string {
	maskOpts := []string{"ro"}
	if m.Writable {
		maskOpts[0] = "rw"
	}

	var specM *spec.Mount
	specM.FromProto(m)
	if specM.Propagation != "" {
		maskOpts = append(maskOpts, specM.Propagation)
	}
	if !m.Populate {
		maskOpts = append(maskOpts, "nocopy")
	}

	switch specM.MCSAccessMode {
	case "private":
		maskOpts = append(maskOpts, "Z")
	case "shared":
		maskOpts = append(maskOpts, "z")
	}

	return strings.Join(maskOpts, ",")
}

func (c *containerConfig) hostConfig() *enginecontainer.HostConfig {
	return &enginecontainer.HostConfig{
		Resources: c.resources(),
		Binds:     c.bindMounts(),
	}
}

func (c *containerConfig) volumeCreateRequest(vol *api.Volume) *types.VolumeCreateRequest {
	return &types.VolumeCreateRequest{
		Name:       vol.Spec.Annotations.Name,
		Driver:     vol.Spec.DriverConfiguration.Name,
		DriverOpts: vol.Spec.DriverConfiguration.Options,
	}
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

func (c *containerConfig) virtualIP(networkID string) string {
	if c.task.Endpoint == nil {
		return ""
	}

	for _, eAttach := range c.task.Endpoint.Attachments {
		// We only support IPv4 VIPs for now.
		if eAttach.NetworkID == networkID && len(eAttach.VirtualIP) > 0 {
			vip, _, err := net.ParseCIDR(eAttach.VirtualIP[0])
			if err != nil {
				return ""
			}

			return vip.String()
		}
	}

	return ""
}

func (c *containerConfig) networkingConfig() *network.NetworkingConfig {
	var networks []*api.NetworkAttachment
	if c.task.GetContainer() != nil {
		networks = c.task.Networks
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
				Name: c.task.ServiceAnnotations.Name,
				ID:   c.task.ServiceID,
				IP:   c.virtualIP(na.Network.ID),
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

	return options, nil
}

func (c containerConfig) eventFilter() filters.Args {
	filter := filters.NewArgs()
	filter.Add("type", events.ContainerEventType)
	filter.Add("name", c.name())
	filter.Add("label", fmt.Sprintf("%v.task.id=%v", systemLabelPrefix, c.task.ID))
	return filter
}
