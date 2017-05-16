package cni

import (
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/allocator/cniallocator"
	"github.com/docker/swarmkit/manager/allocator/networkallocator"
	"github.com/docker/swarmkit/manager/network"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type cni struct{}

// New produces a fresh CNI Network Model
func New() network.Model {
	return &cni{}
}

func (nm *cni) NewAllocator() (networkallocator.NetworkAllocator, error) {
	return cniallocator.New()
}

func (nm *cni) SupportsIngressNetwork() bool {
	return false
}

func (nm *cni) ValidateDriver(driver *api.Driver, pluginType string) error {
	if driver == nil {
		// It is ok to not specify the driver. We will choose
		// a default driver.
		return nil
	}

	// All drivers in CNI mode are "cni".
	if driver.Name != "cni" {
		return grpc.Errorf(codes.InvalidArgument, "driver %s of type %s is not supported in CNI mode", driver.Name, pluginType)
	}

	return nil
}

// PredefinedNetworks returns the list of predefined network structures
func (nm *cni) PredefinedNetworks() []network.PredefinedNetworkData {
	return nil
}
