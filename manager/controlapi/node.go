package controlapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateNodeSpec(spec *api.NodeSpec) error {
	if spec == nil {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	return nil
}

// GetNode returns a Node given a NodeID.
// - Returns `InvalidArgument` if NodeID is not provided.
// - Returns `NotFound` if the Node is not found.
func (s *Server) GetNode(ctx context.Context, request *api.GetNodeRequest) (*api.GetNodeResponse, error) {
	if request.NodeID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var node *api.Node
	s.store.View(func(tx store.ReadTx) {
		node = store.GetNode(tx, request.NodeID)
	})
	if node == nil {
		return nil, grpc.Errorf(codes.NotFound, "node %s not found", request.NodeID)
	}
	return &api.GetNodeResponse{
		Node: node,
	}, nil
}

// ListNodes returns a list of all nodes.
func (s *Server) ListNodes(ctx context.Context, request *api.ListNodesRequest) (*api.ListNodesResponse, error) {
	var (
		nodes []*api.Node
		err   error
	)
	s.store.View(func(tx store.ReadTx) {
		if request.Options == nil || request.Options.Query == "" {
			nodes, err = store.FindNodes(tx, store.All)
		} else {
			nodes, err = store.FindNodes(tx, store.ByQuery(request.Options.Query))
		}
	})

	memberlist := make(map[uint64]*api.RaftMember)
	if s.raft != nil {
		memberlist = s.raft.GetMemberlist()
	}

	list := make([]*api.Node, 0, len(memberlist))
	for _, n := range nodes {
		if n.Manager == nil || memberlist[n.Manager.Raft.RaftID] == nil {
			list = append(list, n)
		} else {

			managerNode := n.Copy()
			// Include live raft status information
			managerNode.Manager.Raft = *memberlist[n.Manager.Raft.RaftID]

			list = append(list, managerNode)
		}
	}

	if err != nil {
		return nil, err
	}
	return &api.ListNodesResponse{
		Nodes: list,
	}, nil
}

// UpdateNode updates a Node referenced by NodeID with the given NodeSpec.
// - Returns `NotFound` if the Node is not found.
// - Returns `InvalidArgument` if the NodeSpec is malformed.
// - Returns an error if the update fails.
func (s *Server) UpdateNode(ctx context.Context, request *api.UpdateNodeRequest) (*api.UpdateNodeResponse, error) {
	if request.NodeID == "" || request.NodeVersion == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	if err := validateNodeSpec(request.Spec); err != nil {
		return nil, err
	}

	var node *api.Node
	err := s.store.Update(func(tx store.Tx) error {
		node = store.GetNode(tx, request.NodeID)
		if node == nil {
			return nil
		}
		node.Meta.Version = *request.NodeVersion
		node.Spec = *request.Spec.Copy()
		return store.UpdateNode(tx, node)
	})
	if err != nil {
		return nil, err
	}
	if node == nil {
		return nil, grpc.Errorf(codes.NotFound, "node %s not found", request.NodeID)
	}
	return &api.UpdateNodeResponse{
		Node: node,
	}, nil
}
