package clusterapi

import (
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
