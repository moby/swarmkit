package csi

import (
	"context"
	"fmt"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

type NodePluginInterface interface {
	NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error)
	NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error
	NodeUnstageVolume(ctx context.Context, req []*api.VolumeAssignment) error
	NodePublishVolume(ctx context.Context, req []*api.VolumeAssignment) error
	NodeUnpublishVolume(ctx context.Context, req []*api.VolumeAssignment) error
}

type volumePublishStatus struct {
	// stagingPath is staging path of volume
	stagingPath string

	// isPublished keeps track if the volume is published.
	isPublished bool

	// publishedPath is published path of volume
	publishedPath string
}

// plugin represents an individual CSI node plugin
type NodePlugin struct {
	// name is the name of the plugin, which is also the name used as the
	// Driver.Name field
	name string

	// node ID is identifier for the node.
	nodeID string

	// socket is the unix socket to connect to this plugin at.
	socket string

	// cc is the grpc client connection
	cc *grpc.ClientConn

	// idClient is the identity service client
	idClient csi.IdentityClient

	// nodeClient is the node service client
	nodeClient csi.NodeClient

	// volumeMap is the map from volume ID to Volume. Will place a volume once it is staged,
	// remove it from the map for unstage.
	// TODO: Make this map persistent if the swarm node goes down
	volumeMap map[string]*volumePublishStatus

	// mu for volumeMap
	mu sync.RWMutex
}

const TargetStagePath string = "/var/lib/docker/stage/%s"

const TargetPublishPath string = "/var/lib/docker/publish/%s"

func NewNodePlugin(name string, nodeID string) *NodePlugin {
	return &NodePlugin{
		name:      name,
		nodeID:    nodeID,
		volumeMap: make(map[string]*volumePublishStatus),
	}
}

// GetPublishedPath returns the path at which the provided volume ID is published.
// Returns an empty string if the volume does not exist.
func (np *NodePlugin) GetPublishedPath(volumeID string) string {
	np.mu.RLock()
	defer np.mu.RUnlock()
	if volInfo, ok := np.volumeMap[volumeID]; ok {
		if volInfo.isPublished {
			return volInfo.publishedPath
		}
	}
	return ""
}

func (np *NodePlugin) NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error) {

	resp := &csi.NodeGetInfoResponse{
		NodeId: np.nodeID,
	}

	return makeNodeInfo(resp), nil
}

func (np *NodePlugin) NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID
	stagingTarget := fmt.Sprintf(TargetStagePath, volID)

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()

	v := &volumePublishStatus{
		stagingPath: stagingTarget,
	}

	np.volumeMap[volID] = v

	return nil
}

func (np *NodePlugin) NodeUnstageVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.FailedPrecondition, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()
	if v, ok := np.volumeMap[volID]; ok {
		if v.isPublished {
			return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not unpublished", volID)
		}
		delete(np.volumeMap, volID)
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}

func (np *NodePlugin) NodePublishVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()
	if v, ok := np.volumeMap[volID]; ok {
		v.publishedPath = fmt.Sprintf(TargetPublishPath, volID)
		v.isPublished = true
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}

func (np *NodePlugin) NodeUnpublishVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()
	if v, ok := np.volumeMap[volID]; ok {
		v.publishedPath = ""
		v.isPublished = false
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}
