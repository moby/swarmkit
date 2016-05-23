package controlapi

import (
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
		node = store.GetNode(readTx, request.NodeID)
	})

	memberlist := s.raft.GetMemberlist()

	for raftID, member := range memberlist {
		if member.NodeID != request.NodeID {
			continue
		}
		err := s.raft.RemoveMember(ctx, raftID)
		if err != nil {
			return nil, grpc.Errorf(codes.Internal, "cannot remove member %s from the cluster: %s", request.NodeID, err)
		}
		return &api.RemoveManagerResponse{}, nil
	}

	if node == nil {
		return nil, grpc.Errorf(codes.NotFound, "node %s not found", request.NodeID)
	}
	return nil, grpc.Errorf(codes.InvalidArgument, "node %s is not a manager", request.NodeID)
}
