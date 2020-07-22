package csi

import (
	"context"

	"google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

type NodePluginInterface interface {
	NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error)
}

// plugin represents an individual CSI node plugin
type NodePlugin struct {
	// Name is the name of the plugin, which is also the name used as the
	// Driver.Name field
	Name string

	// Node ID is identifier for the node.
	NodeID string

	// Socket is the unix socket to connect to this plugin at.
	Socket string

	// CC is the grpc client connection
	CC *grpc.ClientConn

	// IDClient is the identity service client
	IDClient csi.IdentityClient

	// NodeClient is the node service client
	NodeClient csi.NodeClient
}

func (np *NodePlugin) NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error) {
	resp := &csi.NodeGetInfoResponse{
		NodeId: np.NodeID,
	}

	return makeNodeInfo(resp), nil
}
