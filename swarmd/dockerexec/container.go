package dockerexec

import (
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/docker/go-units"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/moby/swarmkit/v2/agent/exec"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/api/genericresource"
	"github.com/moby/swarmkit/v2/api/naming"
	"github.com/moby/swarmkit/v2/template"
)

const (
	// Explicitly use the kernel's default setting for CPU quota of 100ms.
	// https://www.kernel.org/doc/Documentation/scheduler/sched-bwc.txt
	cpuQuotaPeriod = 100 * time.Millisecond

	// systemLabelPrefix represents the reserved namespace for system labels.
	systemLabelPrefix = "com.docker.swarm"
)

// containerConfig converts task properties into docker container compatible
// components.
type containerConfig struct {
	task                *api.Task
	networksAttachments map[string]*api.NetworkAttachment
}

// newContainerConfig returns a validated container config. No methods should
// return an error if this function returns without error.
func newContainerConfig(n *api.NodeDescription, t *api.Task) (*containerConfig, error) {
	var c containerConfig
	return &c, c.setTask(n, t)
}

func (c *containerConfig) setTask(n *api.NodeDescription, t *api.Task) error {
	ctr := t.Spec.GetContainer()
	if ctr == nil {
		return exec.ErrRuntimeUnsupported
	}

	if ctr.Image == "" {
		return ErrImageRequired
	}

	// index the networks by name
	c.networksAttachments = make(map[string]*api.NetworkAttachment, len(t.Networks))
	for _, attachment := range t.Networks {
		c.networksAttachments[attachment.Network.Spec.Annotations.Name] = attachment
	}

	c.task = t
	preparedSpec, err := template.ExpandContainerSpec(n, t)
	if err != nil {
		return err
	}
	c.task.Spec.Runtime = &api.TaskSpec_Container{
		Container: preparedSpec,
	}

	return nil
}

//nolint:unused // TODO(thaJeztah) this is currently unused: is it safe to remove?
func (c *containerConfig) endpoint() *api.Endpoint {
	return c.task.Endpoint
}

func (c *containerConfig) spec() *api.ContainerSpec {
	return c.task.Spec.GetContainer()
}

func (c *containerConfig) name() string {
	return naming.Task(c.task)
}

func (c *containerConfig) image() string {
	return c.spec().Image
}

func portSpec(port uint32, protocol api.PortConfig_Protocol) network.Port {
	p, _ := network.ParsePort(fmt.Sprintf("%d/%s", port, strings.ToLower(protocol.String())))
	return p
}

func (c *containerConfig) portBindings() network.PortMap {
	portBindings := network.PortMap{}
	if c.task.Endpoint == nil {
		return portBindings
	}

	for _, portConfig := range c.task.Endpoint.Ports {
		if portConfig.PublishMode != api.PublishModeHost {
			continue
		}

		port := portSpec(portConfig.TargetPort, portConfig.Protocol)
		binding := []network.PortBinding{
			{},
		}

		if portConfig.PublishedPort != 0 {
			binding[0].HostPort = strconv.Itoa(int(portConfig.PublishedPort))
		}
		portBindings[port] = binding
	}

	return portBindings
}

func (c *containerConfig) isolation() container.Isolation {
	switch c.spec().Isolation {
	case api.ContainerIsolationDefault:
		return "default"
	case api.ContainerIsolationHyperV:
		return "hyperv"
	case api.ContainerIsolationProcess:
		return "process"
	default:
		return ""
	}
}

func (c *containerConfig) exposedPorts() network.PortSet {
	exposedPorts := make(network.PortSet)
	if c.task.Endpoint == nil {
		return exposedPorts
	}

	for _, portConfig := range c.task.Endpoint.Ports {
		if portConfig.PublishMode != api.PublishModeHost {
			continue
		}

		port := portSpec(portConfig.TargetPort, portConfig.Protocol)
		exposedPorts[port] = struct{}{}
	}

	return exposedPorts
}

func (c *containerConfig) config() *container.Config {
	genericEnvs := genericresource.EnvFormat(c.task.AssignedGenericResources, "DOCKER_RESOURCE")
	env := append(c.spec().Env, genericEnvs...)

	config := &container.Config{
		Labels:       c.labels(),
		StopSignal:   c.spec().StopSignal,
		User:         c.spec().User,
		Hostname:     c.spec().Hostname,
		Env:          env,
		WorkingDir:   c.spec().Dir,
		Tty:          c.spec().TTY,
		OpenStdin:    c.spec().OpenStdin,
		Image:        c.image(),
		ExposedPorts: c.exposedPorts(),
		Healthcheck:  c.healthcheck(),
	}

	if len(c.spec().Command) > 0 {
		// If Command is provided, we replace the whole invocation with Command
		// by replacing Entrypoint and specifying Cmd. Args is ignored in this
		// case.
		config.Entrypoint = append(config.Entrypoint, c.spec().Command...)
		config.Cmd = append(config.Cmd, c.spec().Args...)
	} else if len(c.spec().Args) > 0 {
		// In this case, we assume the image has an Entrypoint and Args
		// specifies the arguments for that entrypoint.
		config.Cmd = c.spec().Args
	}

	return config
}

func (c *containerConfig) healthcheck() *container.HealthConfig {
	hcSpec := c.spec().Healthcheck
	if hcSpec == nil {
		return nil
	}
	interval, _ := gogotypes.DurationFromProto(hcSpec.Interval)
	timeout, _ := gogotypes.DurationFromProto(hcSpec.Timeout)
	startPeriod, _ := gogotypes.DurationFromProto(hcSpec.StartPeriod)
	startInterval, _ := gogotypes.DurationFromProto(hcSpec.StartInterval)
	return &container.HealthConfig{
		Test:          hcSpec.Test,
		Interval:      interval,
		Timeout:       timeout,
		Retries:       int(hcSpec.Retries),
		StartPeriod:   startPeriod,
		StartInterval: startInterval,
	}
}

func (c *containerConfig) hostConfig() *container.HostConfig {
	hc := &container.HostConfig{
		Resources:    c.resources(),
		Mounts:       c.mounts(),
		Tmpfs:        c.tmpfs(),
		GroupAdd:     c.spec().Groups,
		PortBindings: c.portBindings(),
		Init:         c.init(),
		Isolation:    c.isolation(),
		CapAdd:       c.spec().CapabilityAdd,
		CapDrop:      c.spec().CapabilityDrop,
		OomScoreAdj:  int(c.spec().OomScoreAdj),
	}

	// The format of extra hosts on swarmkit is specified in:
	// http://man7.org/linux/man-pages/man5/hosts.5.html
	//    IP_address canonical_hostname [aliases...]
	// However, the format of ExtraHosts in HostConfig is
	//    <host>:<ip>
	// We need to do the conversion here
	// (Alias is ignored for now)
	for _, entry := range c.spec().Hosts {
		parts := strings.Fields(entry)
		if len(parts) > 1 {
			hc.ExtraHosts = append(hc.ExtraHosts, fmt.Sprintf("%s:%s", parts[1], parts[0]))
		}
	}

	if c.task.LogDriver != nil {
		hc.LogConfig = container.LogConfig{
			Type:   c.task.LogDriver.Name,
			Config: c.task.LogDriver.Options,
		}
	}

	return hc
}

func (c *containerConfig) labels() map[string]string {
	system := map[string]string{
		"task":         "", // mark as cluster task
		"task.id":      c.task.ID,
		"task.name":    naming.Task(c.task),
		"node.id":      c.task.NodeID,
		"service.id":   c.task.ServiceID,
		"service.name": c.task.ServiceAnnotations.Name,
	}

	// base labels are those defined in the spec.
	labels := make(map[string]string)
	maps.Copy(labels, c.spec().Labels)

	// we then apply the overrides from the task, which may be set via the
	// orchestrator.
	maps.Copy(labels, c.task.Annotations.Labels)

	// finally, we apply the system labels, which override all labels.
	for k, v := range system {
		labels[strings.Join([]string{systemLabelPrefix, k}, ".")] = v
	}

	return labels
}

func (c *containerConfig) tmpfs() map[string]string {
	r := make(map[string]string)

	for _, spec := range c.spec().Mounts {
		if spec.Type != api.MountTypeTmpfs {
			continue
		}

		r[spec.Target] = getMountMask(&spec)
	}

	return r
}

func (c *containerConfig) mounts() []mount.Mount {
	var r []mount.Mount
	for _, mnt := range c.spec().Mounts {
		r = append(r, convertMount(mnt))
	}
	return r
}

func convertMount(m api.Mount) mount.Mount {
	mnt := mount.Mount{
		Source:   m.Source,
		Target:   m.Target,
		ReadOnly: m.ReadOnly,
	}

	switch m.Type {
	case api.MountTypeBind:
		mnt.Type = mount.TypeBind
	case api.MountTypeVolume:
		mnt.Type = mount.TypeVolume
	case api.MountTypeNamedPipe:
		mnt.Type = mount.TypeNamedPipe
	}

	if m.BindOptions != nil {
		mnt.BindOptions = &mount.BindOptions{
			NonRecursive:           m.BindOptions.NonRecursive,
			CreateMountpoint:       m.BindOptions.CreateMountpoint,
			ReadOnlyNonRecursive:   m.BindOptions.ReadOnlyNonRecursive,
			ReadOnlyForceRecursive: m.BindOptions.ReadOnlyForceRecursive,
		}
		switch m.BindOptions.Propagation {
		case api.MountPropagationRPrivate:
			mnt.BindOptions.Propagation = mount.PropagationRPrivate
		case api.MountPropagationPrivate:
			mnt.BindOptions.Propagation = mount.PropagationPrivate
		case api.MountPropagationRSlave:
			mnt.BindOptions.Propagation = mount.PropagationRSlave
		case api.MountPropagationSlave:
			mnt.BindOptions.Propagation = mount.PropagationSlave
		case api.MountPropagationRShared:
			mnt.BindOptions.Propagation = mount.PropagationRShared
		case api.MountPropagationShared:
			mnt.BindOptions.Propagation = mount.PropagationShared
		}
	}

	if m.VolumeOptions != nil {
		mnt.VolumeOptions = &mount.VolumeOptions{
			NoCopy: m.VolumeOptions.NoCopy,
			// TODO: uncomment after 26.0 vendor
			// Subpath: m.VolumeOptions.Subpath,
			Labels: maps.Clone(m.VolumeOptions.Labels),
		}
		if m.VolumeOptions.DriverConfig != nil {
			mnt.VolumeOptions.DriverConfig = &mount.Driver{
				Name:    m.VolumeOptions.DriverConfig.Name,
				Options: maps.Clone(m.VolumeOptions.DriverConfig.Options),
			}
		}
	}
	return mnt
}

func getMountMask(m *api.Mount) string {
	var maskOpts []string
	if m.ReadOnly {
		maskOpts = append(maskOpts, "ro")
	}

	switch m.Type {
	case api.MountTypeTmpfs:
		if m.TmpfsOptions == nil {
			break
		}

		if m.TmpfsOptions.Mode != 0 {
			maskOpts = append(maskOpts, fmt.Sprintf("mode=%o", m.TmpfsOptions.Mode))
		}

		if m.TmpfsOptions.SizeBytes != 0 {
			// calculate suffix here, making this linux specific, but that is
			// okay, since API is that way anyways.

			// we do this by finding the suffix that divides evenly into the
			// value, returning the value itself, with no suffix, if it fails.
			//
			// For the most part, we don't enforce any semantic to this values.
			// The operating system will usually align this and enforce minimum
			// and maximums.
			var (
				size   = m.TmpfsOptions.SizeBytes
				suffix string
			)
			for _, r := range []struct {
				suffix  string
				divisor int64
			}{
				{"g", 1 << 30},
				{"m", 1 << 20},
				{"k", 1 << 10},
			} {
				if size%r.divisor == 0 {
					size = size / r.divisor
					suffix = r.suffix
					break
				}
			}

			maskOpts = append(maskOpts, fmt.Sprintf("size=%d%s", size, suffix))
		}

		if opts := m.TmpfsOptions.Options; opts != "" {
			validOpts := map[string]bool{
				"exec":   true,
				"noexec": true,
			}
			for opt := range strings.SplitSeq(strings.ToLower(opts), ",") {
				if _, ok := validOpts[opt]; ok {
					maskOpts = append(maskOpts, opt)
				}
			}
		}
	}

	return strings.Join(maskOpts, ",")
}

// This handles the case of volumes that are defined inside a service Mount
func (c *containerConfig) volumeCreateRequest(mount *api.Mount) *client.VolumeCreateOptions {
	var (
		driverName string
		driverOpts map[string]string
		labels     map[string]string
	)

	if mount.VolumeOptions != nil && mount.VolumeOptions.DriverConfig != nil {
		driverName = mount.VolumeOptions.DriverConfig.Name
		driverOpts = mount.VolumeOptions.DriverConfig.Options
		labels = mount.VolumeOptions.Labels
	}

	// FIXME: do we need the ClusterVolumeSpec here?
	return &client.VolumeCreateOptions{
		Name:       mount.Source,
		Driver:     driverName,
		DriverOpts: driverOpts,
		Labels:     labels,
	}
}

func (c *containerConfig) resources() container.Resources {
	resources := container.Resources{}

	// set pids limit
	pidsLimit := c.spec().PidsLimit
	if pidsLimit > 0 {
		resources.PidsLimit = &pidsLimit
	}

	resources.Ulimits = make([]*units.Ulimit, len(c.spec().Ulimits))
	for i, ulimit := range c.spec().Ulimits {
		resources.Ulimits[i] = &units.Ulimit{
			Name: ulimit.Name,
			Soft: ulimit.Soft,
			Hard: ulimit.Hard,
		}
	}

	// If no limits are specified let the engine use its defaults.
	//
	// TODO(aluzzardi): We might want to set some limits anyway otherwise
	// "unlimited" tasks will step over the reservation of other tasks.
	r := c.task.Spec.Resources
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

//nolint:unused // TODO(thaJeztah) this is currently unused: is it safe to remove?
func (c *containerConfig) virtualIP(networkID string) string {
	if c.task.Endpoint == nil {
		return ""
	}

	for _, vip := range c.task.Endpoint.VirtualIPs {
		// We only support IPv4 VIPs for now.
		if vip.NetworkID == networkID {
			vip, _, err := net.ParseCIDR(vip.Addr)
			if err != nil {
				return ""
			}

			return vip.String()
		}
	}

	return ""
}

func (c *containerConfig) networkingConfig() *network.NetworkingConfig {
	epConfig := make(map[string]*network.EndpointSettings)
	for _, na := range c.task.Networks {
		var ipv4, ipv6 netip.Addr
		for _, addr := range na.Addresses {
			prefix, err := netip.ParsePrefix(addr)
			if err != nil {
				continue
			}

			ip := prefix.Addr()
			if ip.Is4() {
				ipv4 = ip
				continue
			}
			if ip.Is6() {
				ipv6 = ip
			}
		}

		epSettings := &network.EndpointSettings{
			IPAMConfig: &network.EndpointIPAMConfig{
				IPv4Address: ipv4,
				IPv6Address: ipv6,
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

func (c *containerConfig) networkCreateOptions(name string) (client.NetworkCreateOptions, error) {
	na, ok := c.networksAttachments[name]
	if !ok {
		return client.NetworkCreateOptions{}, errors.New("container: unknown network referenced")
	}

	options := client.NetworkCreateOptions{
		Driver: na.Network.DriverState.Name,
		IPAM: &network.IPAM{
			Driver: na.Network.IPAM.Driver.Name,
		},
		Options: na.Network.DriverState.Options,
	}

	for _, ic := range na.Network.IPAM.Configs {
		sn, err := netip.ParsePrefix(ic.Subnet)
		if err != nil {
			continue
		}
		r, err := netip.ParsePrefix(ic.Range)
		if err != nil {
			continue
		}
		gw, err := netip.ParseAddr(ic.Gateway)
		if err != nil {
			continue
		}
		options.IPAM.Config = append(options.IPAM.Config, network.IPAMConfig{
			Subnet:  sn,
			IPRange: r,
			Gateway: gw,
		})
	}

	return options, nil
}

func (c containerConfig) eventFilter() client.Filters {
	return make(client.Filters).
		Add("type", string(events.ContainerEventType)).
		Add("name", c.name()).
		Add("label", fmt.Sprintf("%v.task.id=%v", systemLabelPrefix, c.task.ID))
}

func (c *containerConfig) init() *bool {
	if c.spec().Init != nil {
		return &c.spec().Init.Value
	}
	return nil
}
