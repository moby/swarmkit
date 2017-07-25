package cni

import (
	"github.com/containernetworking/cni/libcni"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/allocator/cniallocator"
	"github.com/docker/swarmkit/manager/network"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type cni struct{}

// New produces a fresh CNI Network Model
func New() network.Model {
	return &cni{}
}

func (nm *cni) NewAllocator() (network.Allocator, error) {
	return cniallocator.New()
}

func (nm *cni) SupportsIngressNetwork() bool {
	return false
}

func parseCNIspec(spec *api.NetworkSpec) (*libcni.NetworkConfig, error) {
	// This is rather similar to cniConfig in the containerd executor...
	cniConfig, ok := spec.DriverConfig.Options["config"]
	if !ok {
		return nil, grpc.Errorf(codes.InvalidArgument, "CNI network has no config")
	}

	cni, err := libcni.ConfFromBytes([]byte(cniConfig))
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "Failed to parse CNI config: %s", err)
	}
	return cni, nil
}

func (nm *cni) ValidateNetworkSpec(ctx context.Context, spec *api.NetworkSpec) error {
	if spec.DriverConfig == nil || spec.DriverConfig.Name != "cni" {
		return grpc.Errorf(codes.InvalidArgument, "spec is not for a CNI network")
	}

	cni, err := parseCNIspec(spec)
	if err != nil {
		return err
	}

	if spec.Annotations.Name != cni.Network.Name {
		log.G(ctx).Infof("CNI Network name (%q) does not match Spec name (%q)",
			cni.Network.Name, spec.Annotations.Name)
	}

	if spec.IPAM != nil {
		return grpc.Errorf(codes.InvalidArgument, "CNI networks cannot have IPAM")
	}

	return nil
}

// PredefinedNetworks returns the list of predefined network structures
func (nm *cni) PredefinedNetworks() []network.PredefinedNetworkData {
	return nil
}