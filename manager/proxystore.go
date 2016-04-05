package manager

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/stateutil"
	"golang.org/x/net/context"
)

// NodeReady notifies that node is ready to accept request. Only cluster
// leader can process this request.
func (m *Manager) NodeReady(ctx context.Context, r *api.NodeReadyRequest) (*api.NodeReadyResponse, error) {
	if !m.raftNode.IsLeader() {
		return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
	}
	node, err := stateutil.NodeReady(m.raftNode.MemoryStore(), r.NodeID, r.Description)
	if err != nil {
		if err == state.ErrLostLeadership {
			return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
		}
		return nil, err
	}
	return &api.NodeReadyResponse{
		Node: node,
	}, nil
}

// UpdateTasks used to update tasks statuses. Only cluster leader can
// process this request.
func (m *Manager) UpdateTasks(ctx context.Context, r *api.UpdateTasksRequest) (*api.UpdateTasksResponse, error) {
	if !m.raftNode.IsLeader() {
		return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
	}
	err := stateutil.UpdateTasks(m.raftNode.MemoryStore(), r.Updates)
	if err != nil {
		if err == state.ErrLostLeadership {
			return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
		}
		return nil, err
	}
	return nil, nil
}

// UpdateNodeStatus updates only status of specific node. Only cluster leader
// can process this request.
func (m *Manager) UpdateNodeStatus(ctx context.Context, r *api.UpdateNodeStatusRequest) (*api.UpdateNodeStatusResponse, error) {
	if !m.raftNode.IsLeader() {
		return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
	}
	err := stateutil.UpdateNodeStatus(m.raftNode.MemoryStore(), r.NodeID, r.Status)
	if err != nil {
		if err == state.ErrLostLeadership {
			return nil, grpc.Errorf(codes.FailedPrecondition, state.ErrLostLeadership.Error())
		}
		return nil, err
	}
	return nil, nil
}
