package cnm

import (
	"net"
	"strings"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/libnetwork/driverapi"
	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/cnmallocator"
	"github.com/docker/swarmkit/manager/network"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type cnm struct {
	pg plugingetter.PluginGetter
}

// New produces a fresh CNM Network Model
func New(pg plugingetter.PluginGetter) network.Model {
	return &cnm{
		pg: pg,
	}
}

func (nm *cnm) NewAllocator() (network.Allocator, error) {
	return cnmallocator.New(nm.pg)
}

func (nm *cnm) SupportsIngressNetwork() bool {
	return true
}

func (nm *cnm) ValidateNetworkSpec(spec *api.NetworkSpec) error {
	if spec.Ingress && spec.DriverConfig != nil && spec.DriverConfig.Name != "overlay" {
		return grpc.Errorf(codes.Unimplemented, "only overlay driver is currently supported for ingress network")
	}

	if err := nm.validateDriver(spec.DriverConfig, driverapi.NetworkPluginEndpointType); err != nil {
		return err
	}

	if err := nm.validateIPAM(spec.IPAM); err != nil {
		return err
	}

	return nil
}

func (nm *cnm) SetDefaults(spec *api.NetworkSpec) error {
	return nil
}

func (nm *cnm) validateDriver(driver *api.Driver, pluginType string) error {
	if driver == nil {
		// It is ok to not specify the driver. We will choose
		// a default driver.
		return nil
	}

	if driver.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "driver name: if driver is specified name is required")
	}

	// First check against the known drivers
	switch pluginType {
	case ipamapi.PluginEndpointType:
		if strings.ToLower(driver.Name) == ipamapi.DefaultIPAM {
			return nil
		}
	case driverapi.NetworkPluginEndpointType:
		if cnmallocator.IsBuiltInDriver(driver.Name) {
			return nil
		}
	}

	if nm.pg == nil {
		return grpc.Errorf(codes.InvalidArgument, "plugin %s not supported", driver.Name)
	}

	p, err := nm.pg.Get(driver.Name, pluginType, plugingetter.Lookup)
	if err != nil {
		return grpc.Errorf(codes.InvalidArgument, "error during lookup of plugin %s", driver.Name)
	}

	if p.IsV1() {
		return grpc.Errorf(codes.InvalidArgument, "legacy plugin %s of type %s is not supported in swarm mode", driver.Name, pluginType)
	}

	return nil
}

func validateIPAMConfiguration(ipamConf *api.IPAMConfig) error {
	if ipamConf == nil {
		return grpc.Errorf(codes.InvalidArgument, "ipam configuration: cannot be empty")
	}

	_, subnet, err := net.ParseCIDR(ipamConf.Subnet)
	if err != nil {
		return grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid subnet %s", ipamConf.Subnet)
	}

	if ipamConf.Range != "" {
		ip, _, err := net.ParseCIDR(ipamConf.Range)
		if err != nil {
			return grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid range %s", ipamConf.Range)
		}

		if !subnet.Contains(ip) {
			return grpc.Errorf(codes.InvalidArgument, "ipam configuration: subnet %s does not contain range %s", ipamConf.Subnet, ipamConf.Range)
		}
	}

	if ipamConf.Gateway != "" {
		ip := net.ParseIP(ipamConf.Gateway)
		if ip == nil {
			return grpc.Errorf(codes.InvalidArgument, "ipam configuration: invalid gateway %s", ipamConf.Gateway)
		}

		if !subnet.Contains(ip) {
			return grpc.Errorf(codes.InvalidArgument, "ipam configuration: subnet %s does not contain gateway %s", ipamConf.Subnet, ipamConf.Gateway)
		}
	}

	return nil
}

func (nm *cnm) validateIPAM(ipam *api.IPAMOptions) error {
	if ipam == nil {
		// It is ok to not specify any IPAM configurations. We
		// will choose good defaults.
		return nil
	}

	if err := nm.validateDriver(ipam.Driver, ipamapi.PluginEndpointType); err != nil {
		return err
	}

	for _, ipamConf := range ipam.Configs {
		if err := validateIPAMConfiguration(ipamConf); err != nil {
			return err
		}
	}

	return nil
}
