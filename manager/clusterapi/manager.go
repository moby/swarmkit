package clusterapi

import (
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// ListManagers returns a list of all the managers.
func (s *Server) ListManagers(ctx context.Context, request *api.ListManagersRequest) (*api.ListManagersResponse, error) {
	memberlist := s.raft.GetMemberlist()

	list := make([]*api.Member, 0, len(memberlist))
	for _, v := range memberlist {
		list = append(list, v)
	}

	return &api.ListManagersResponse{
		Managers: list,
	}, nil
}

// RemoveManager removes a manager from the cluster.
func (s *Server) RemoveManager(ctx context.Context, request *api.RemoveManagerRequest) (*api.RemoveManagerResponse, error) {
	memberlist := s.raft.GetMemberlist()

	removeID, err := strconv.ParseUint(request.ManagerID, 16, 64)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, err.Error())
	}

	if _, exists := memberlist[removeID]; !exists {
		return nil, grpc.Errorf(codes.NotFound, "member %s not found", request.ManagerID)
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	err = s.raft.RemoveMember(ctx, removeID)
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, "cannot remove member %s from the cluster: %s", request.ManagerID, err)
	}

	return &api.RemoveManagerResponse{}, nil
}
