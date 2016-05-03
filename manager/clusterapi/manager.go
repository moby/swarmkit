package clusterapi

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// GetManager returns a manager given a MangerID
// - Returns `InvalidArgument` if NodeID is not provided.
// - Returns `NotFound` if the Node is not found.
func (s *Server) GetManager(ctx context.Context, request *api.GetManagerRequest) (*api.GetManagerResponse, error) {
	if request.ManagerID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	// TODO(nishanttotla): Perhaps a separate function to find the right manager should be created
	managerlist := s.raft.GetMemberlist()
	var manager *api.Member
	for _, v := range managerlist {
		if v.ID == request.ManagerID {
			manager = v
			break
		}
	}

	if manager == nil {
		return nil, grpc.Errorf(codes.NotFound, "manager %s not found", request.ManagerID)
	}

	return &api.GetManagerResponse{
		Manager: manager,
	}, nil
}

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
