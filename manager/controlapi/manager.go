package controlapi

import (
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
)

// RemoveManager removes a manager from the cluster.
func (s *Server) RemoveManager(ctx context.Context, request *api.RemoveManagerRequest) (*api.RemoveManagerResponse, error) {
	var node *api.Node
	s.raft.MemoryStore().View(func(readTx store.ReadTx) {
		node = store.GetNode(readTx, request.ManagerID)
	})
	if node == nil {
		return nil, grpc.Errorf(codes.NotFound, "node %s not found", request.ManagerID)
	}
	if node.Manager == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "node %s is not a manager", request.ManagerID)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err := s.raft.RemoveMember(ctx, node.Manager.Raft.RaftID)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "cannot remove member %s from the cluster: %s", request.ManagerID, err)
	}

	return &api.RemoveManagerResponse{}, nil
}
