package plugin

import (
	"context"
	"path/filepath"
	"sync"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

type NodePlugin interface {
	GetPublishedPath(volumeID string) string
	NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error)
	NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error
	NodeUnstageVolume(ctx context.Context, req *api.VolumeAssignment) error
	NodePublishVolume(ctx context.Context, req *api.VolumeAssignment) error
	NodeUnpublishVolume(ctx context.Context, req *api.VolumeAssignment) error
}

type volumePublishStatus struct {
	// stagingPath is staging path of volume
	stagingPath string

	// isPublished keeps track if the volume is published.
	isPublished bool

	// publishedPath is published path of volume
	publishedPath string
}

type nodePlugin struct {
	// name is the name of the plugin, which is used in the Driver.Name field.
	name string

	// socket is the path of the unix socket to connect to this plugin at
	socket string

	// volumeMap is the map from volume ID to Volume. Will place a volume once it is staged,
	// remove it from the map for unstage.
	// TODO: Make this map persistent if the swarm node goes down
	volumeMap map[string]*volumePublishStatus

	// mu for volumeMap
	mu sync.RWMutex

	// staging indicates that the plugin has staging capabilities.
	staging bool

	// cc is the gRPC client connection
	cc *grpc.ClientConn

	// idClient is the CSI Identity Service client
	idClient csi.IdentityClient

	// nodeClient is the CSI Node Service client
	nodeClient csi.NodeClient
}

const TargetStagePath string = "/var/lib/docker/stage"
const TargetPublishPath string = "/var/lib/docker/publish"

func NewNodePlugin(name, socket string) NodePlugin {
	return newNodePlugin(name, socket)
}

// newNodePlugin returns a raw nodePlugin object, not behind an interface. this
// is useful for testing.
func newNodePlugin(name, socket string) *nodePlugin {
	return &nodePlugin{
		name:      name,
		socket:    socket,
		volumeMap: map[string]*volumePublishStatus{},
	}
}

// connect is a private method that sets up the identity client and node
// client from a grpc client. it exists separately so that testing code can
// substitute in fake clients without a grpc connection
func (np *nodePlugin) connect(ctx context.Context) error {
	cc, err := grpc.DialContext(ctx, np.socket)
	if err != nil {
		return err
	}

	np.cc = cc
	// first, probe the plugin, to ensure that it exists and is ready to go
	idc := csi.NewIdentityClient(cc)
	np.idClient = idc

	np.nodeClient = csi.NewNodeClient(cc)

	return nil
}

func (np *nodePlugin) Client(ctx context.Context) (csi.NodeClient, error) {
	if np.nodeClient == nil {
		if err := np.connect(ctx); err != nil {
			return nil, err
		}
	}
	return np.nodeClient, nil
}

func (np *nodePlugin) init(ctx context.Context) error {
	probe, err := np.idClient.Probe(ctx, &csi.ProbeRequest{})
	if err != nil {
		return err
	}
	if probe.Ready != nil && !probe.Ready.Value {
		return status.Error(codes.FailedPrecondition, "Plugin is not Ready")
	}

	c, err := np.Client(ctx)
	if err != nil {
		return err
	}

	resp, err := c.NodeGetCapabilities(ctx, &csi.NodeGetCapabilitiesRequest{})
	if err != nil {
		// TODO(ameyag): handle
		return err
	}
	if resp == nil {
		return nil
	}
	for _, c := range resp.Capabilities {
		if rpc := c.GetRpc(); rpc != nil {
			switch rpc.Type {
			case csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME:
				np.staging = true
			}
		}
	}

	return nil
}

// GetPublishedPath returns the path at which the provided volume ID is published.
// Returns an empty string if the volume does not exist.
func (np *nodePlugin) GetPublishedPath(volumeID string) string {
	np.mu.RLock()
	defer np.mu.RUnlock()
	if volInfo, ok := np.volumeMap[volumeID]; ok {
		if volInfo.isPublished {
			return volInfo.publishedPath
		}
	}
	return ""
}

func (np *nodePlugin) NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error) {
	c, err := np.Client(ctx)
	if err != nil {
		return nil, err
	}
	resp, err := c.NodeGetInfo(ctx, &csi.NodeGetInfoRequest{})
	if err != nil {
		return nil, err
	}

	return makeNodeInfo(resp), nil
}

func (np *nodePlugin) NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error {

	np.mu.Lock()
	defer np.mu.Unlock()
	if !np.staging {
		return nil
	}

	volID := req.VolumeID
	stagingTarget := filepath.Join(TargetStagePath, volID)

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	c, err := np.Client(ctx)
	if err != nil {
		return err
	}

	_, err = c.NodeStageVolume(ctx, &csi.NodeStageVolumeRequest{
		VolumeId:          volID,
		StagingTargetPath: stagingTarget,
	})

	if err != nil {
		return err
	}

	v := &volumePublishStatus{
		stagingPath: stagingTarget,
	}

	np.volumeMap[volID] = v

	return nil
}

func (np *nodePlugin) NodeUnstageVolume(ctx context.Context, req *api.VolumeAssignment) error {

	np.mu.Lock()
	defer np.mu.Unlock()
	if !np.staging {
		return nil
	}

	volID := req.VolumeID
	stagingTarget := filepath.Join(TargetStagePath, volID)

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.FailedPrecondition, "Volume ID missing in request")
	}

	c, err := np.Client(ctx)
	if err != nil {
		return err
	}

	_, err = c.NodeUnstageVolume(ctx, &csi.NodeUnstageVolumeRequest{
		VolumeId:          volID,
		StagingTargetPath: stagingTarget,
	})

	if err != nil {
		return err
	}

	if v, ok := np.volumeMap[volID]; ok {
		if v.isPublished {
			return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not unpublished", volID)
		}
		delete(np.volumeMap, volID)
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}

func (np *nodePlugin) NodePublishVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()

	publishPath := filepath.Join(TargetPublishPath, volID)

	c, err := np.Client(ctx)
	if err != nil {
		return err
	}

	_, err = c.NodePublishVolume(ctx, &csi.NodePublishVolumeRequest{
		VolumeId:   volID,
		TargetPath: publishPath,
	})

	if err != nil {
		return err
	}

	if v, ok := np.volumeMap[volID]; ok {
		v.publishedPath = publishPath
		v.isPublished = true
		return nil
	} else if !np.staging {
		// If staging is not supported on plugin, we need to add volume to the map.
		v := &volumePublishStatus{
			publishedPath: publishPath,
			isPublished:   true,
		}
		np.volumeMap[volID] = v
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}

func (np *nodePlugin) NodeUnpublishVolume(ctx context.Context, req *api.VolumeAssignment) error {

	volID := req.VolumeID

	// Check arguments
	if len(volID) == 0 {
		return status.Error(codes.InvalidArgument, "Volume ID missing in request")
	}

	np.mu.Lock()
	defer np.mu.Unlock()
	publishPath := filepath.Join(TargetPublishPath, volID)

	c, err := np.Client(ctx)
	if err != nil {
		return err
	}

	_, err = c.NodeUnpublishVolume(ctx, &csi.NodeUnpublishVolumeRequest{
		VolumeId:   volID,
		TargetPath: publishPath,
	})

	if err != nil {
		return err
	}

	if v, ok := np.volumeMap[volID]; ok {
		v.publishedPath = ""
		v.isPublished = false
		return nil
	}

	return status.Errorf(codes.FailedPrecondition, "VolumeID %s is not staged", volID)
}

// makeNodeInfo converts a csi.NodeGetInfoResponse object into a swarmkit NodeCSIInfo
// object.
func makeNodeInfo(csiNodeInfo *csi.NodeGetInfoResponse) *api.NodeCSIInfo {
	return &api.NodeCSIInfo{
		NodeID:            csiNodeInfo.NodeId,
		MaxVolumesPerNode: csiNodeInfo.MaxVolumesPerNode,
	}
}
