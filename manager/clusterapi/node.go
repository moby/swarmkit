package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/state"
	"golang.org/x/net/context"
)

// ListNodes returns a list of all nodes.
func (s *Server) ListNodes(ctx context.Context, request *api.ListNodesRequest) (*api.ListNodesResponse, error) {
	var nodes []*api.Node
	err := s.store.View(func(tx state.ReadTx) error {
		var err error

		nodes, err = tx.Nodes().Find(state.All)
		return err
	})
	if err != nil {
		return nil, err
	}
	return &api.ListNodesResponse{
		Nodes: nodes,
	}, nil
}
