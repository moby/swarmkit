package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateMeta(m api.Meta) error {
	if m.Name == "" {
		return grpc.Errorf(codes.InvalidArgument, "meta: name must be provided")
	}
	return nil
}
