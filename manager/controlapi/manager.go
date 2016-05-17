package controlapi

import (
	"strconv"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// GetManager returns a manager given a ManagerID
// - Returns `InvalidArgument` if ManagerID is not provided
// - Returns `NotFound` if the Manager is not found
func (s *Server) GetManager(ctx context.Context, request *api.GetManagerRequest) (*api.GetManagerResponse, error) {
	if request.ManagerID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	// TODO(nishanttotla): Perhaps a separate function to find the right manager should be created
	listmanagers, err := s.ListManagers(ctx, &api.ListManagersRequest{})
	if err != nil {
		return nil, err
	}

	managerlist := listmanagers.Managers
	var manager *api.Manager
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

	list := make([]*api.Manager, 0, len(memberlist))
	for _, v := range memberlist {
		// TODO(aaronl): These Manager structs will need to contain
		// actual node IDs, not stringified versions of the raft ID.
		list = append(list, &api.Manager{ID: strconv.FormatUint(v.RaftID, 16), Raft: *v})
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
