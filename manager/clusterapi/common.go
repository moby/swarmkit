package clusterapi

import (
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateMeta(m specspb.Meta) error {
	if m.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "meta: name must be provided")
	}
	return nil
}

func validateDriver(driver *typespb.Driver) error {
	if driver == nil {
		// It is ok to not specify the driver. We will choose
		// a default driver.
		return nil
	}

	if driver.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "driver name: if driver is specified name is required")
	}

	return nil
}
