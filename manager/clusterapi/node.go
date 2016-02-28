package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// ListNodes returns a list of all nodes.
func (s *Server) ListNodes(ctx context.Context, request *api.ListNodesRequest) (*api.ListNodesResponse, error) {
	return &api.ListNodesResponse{
		Nodes: s.store.Nodes(),
	}, nil
}
