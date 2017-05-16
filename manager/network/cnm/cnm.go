package cnm

import (
	"strings"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/libnetwork/driverapi"
	"github.com/docker/libnetwork/ipamapi"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/cnmallocator"
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
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

func (nm *cnm) NewAllocator() (networkallocator.NetworkAllocator, error) {
	return cnmallocator.New(nm.pg)
}

func (nm *cnm) SupportsIngressNetwork() bool {
	return true
}

func (nm *cnm) ValidateDriver(driver *api.Driver, pluginType string) error {
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
