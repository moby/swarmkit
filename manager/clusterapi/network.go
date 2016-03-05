package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// CreateNetwork creates and return a Network based on the provided NetworkSpec.
// TODO(aluzzardi): Not implemented.
func (s *Server) CreateNetwork(ctx context.Context, request *api.CreateNetworkRequest) (*api.CreateNetworkResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// GetNetwork returns a Network given a NetworkID.
// - Returns `InvalidArgument` if NetworkID is not provided.
// - Returns `NotFound` if the Network is not found.
// TODO(aluzzardi): Not implemented.
func (s *Server) GetNetwork(ctx context.Context, request *api.GetNetworkRequest) (*api.GetNetworkResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// RemoveNetwork removes a Network referenced by NetworkID.
// TODO(aluzzardi): Not implemented.
func (s *Server) RemoveNetwork(ctx context.Context, request *api.RemoveNetworkRequest) (*api.RemoveNetworkResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}

// ListNetworks returns a list of all networks.
// TODO(aluzzardi): Not implemented.
func (s *Server) ListNetworks(ctx context.Context, request *api.ListNetworksRequest) (*api.ListNetworksResponse, error) {
	return nil, grpc.Errorf(codes.Unimplemented, errNotImplemented.Error())
}
