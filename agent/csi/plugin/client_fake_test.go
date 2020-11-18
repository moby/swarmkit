package plugin

import (
	"context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

type fakeNodeClient struct {
	// stagedVolumes is a set of all volume IDs for which NodeStageVolume has been
	// called on this fake
	stagedVolumes map[string]struct{}

	// getInfoRequests is a log of all requests to NodeGetInfo.
	getInfoRequests []*csi.NodeGetInfoRequest
	// stageVolumeRequests is a log of all requests to NodeStageVolume.
	stageVolumeRequests []*csi.NodeStageVolumeRequest
	// unstageVolumeRequests is a log of all requests to NodeUnstageVolume.
	unstageVolumeRequests []*csi.NodeUnstageVolumeRequest
	// publishVolumeRequests is a log of all requests to NodePublishVolume.
	publishVolumeRequests []*csi.NodePublishVolumeRequest
	// unpublishVolumeRequests is a log of all requests to NodeUnpublishVolume.
	unpublishVolumeRequests []*csi.NodeUnpublishVolumeRequest
	// getCapabilitiesRequests is a log of all requests to NodeGetInfo.
	getCapabilitiesRequests []*csi.NodeGetCapabilitiesRequest
	// idCounter is a simple way to generate ids
	idCounter int
	// isStaging indicates if plugin supports stage/unstage capability
	isStaging bool
	// node ID is identifier for the node.
	nodeID string
}

func newFakeNodeClient(isStaging bool, nodeID string) *fakeNodeClient {
	return &fakeNodeClient{
		stagedVolumes:           map[string]struct{}{},
		getInfoRequests:         []*csi.NodeGetInfoRequest{},
		stageVolumeRequests:     []*csi.NodeStageVolumeRequest{},
		unstageVolumeRequests:   []*csi.NodeUnstageVolumeRequest{},
		publishVolumeRequests:   []*csi.NodePublishVolumeRequest{},
		unpublishVolumeRequests: []*csi.NodeUnpublishVolumeRequest{},
		getCapabilitiesRequests: []*csi.NodeGetCapabilitiesRequest{},
		isStaging:               isStaging,
		nodeID:                  nodeID,
	}
}

func (f *fakeNodeClient) NodeGetInfo(ctx context.Context, in *csi.NodeGetInfoRequest, _ ...grpc.CallOption) (*csi.NodeGetInfoResponse, error) {

	f.idCounter++
	f.getInfoRequests = append(f.getInfoRequests, in)
	return &csi.NodeGetInfoResponse{
		NodeId: f.nodeID,
	}, nil
}

func (f *fakeNodeClient) NodeStageVolume(ctx context.Context, in *csi.NodeStageVolumeRequest, opts ...grpc.CallOption) (*csi.NodeStageVolumeResponse, error) {
	f.idCounter++
	f.stageVolumeRequests = append(f.stageVolumeRequests, in)
	f.stagedVolumes[in.VolumeId] = struct{}{}

	return &csi.NodeStageVolumeResponse{}, nil
}

func (f *fakeNodeClient) NodeUnstageVolume(ctx context.Context, in *csi.NodeUnstageVolumeRequest, opts ...grpc.CallOption) (*csi.NodeUnstageVolumeResponse, error) {
	f.idCounter++
	f.unstageVolumeRequests = append(f.unstageVolumeRequests, in)

	if _, ok := f.stagedVolumes[in.VolumeId]; !ok {
		return nil, status.Error(codes.FailedPrecondition, "can't unstage volume that is not already staged")
	}

	delete(f.stagedVolumes, in.VolumeId)

	return &csi.NodeUnstageVolumeResponse{}, nil
}

func (f *fakeNodeClient) NodePublishVolume(ctx context.Context, in *csi.NodePublishVolumeRequest, opts ...grpc.CallOption) (*csi.NodePublishVolumeResponse, error) {
	f.idCounter++
	f.publishVolumeRequests = append(f.publishVolumeRequests, in)

	return &csi.NodePublishVolumeResponse{}, nil
}

func (f *fakeNodeClient) NodeUnpublishVolume(ctx context.Context, in *csi.NodeUnpublishVolumeRequest, opts ...grpc.CallOption) (*csi.NodeUnpublishVolumeResponse, error) {
	f.idCounter++
	f.unpublishVolumeRequests = append(f.unpublishVolumeRequests, in)
	return &csi.NodeUnpublishVolumeResponse{}, nil

}

func (f *fakeNodeClient) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest, opts ...grpc.CallOption) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeExpandVolume(ctx context.Context, in *csi.NodeExpandVolumeRequest, opts ...grpc.CallOption) (*csi.NodeExpandVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeGetCapabilities(ctx context.Context, in *csi.NodeGetCapabilitiesRequest, opts ...grpc.CallOption) (*csi.NodeGetCapabilitiesResponse, error) {
	f.idCounter++
	f.getCapabilitiesRequests = append(f.getCapabilitiesRequests, in)
	if f.isStaging {
		return &csi.NodeGetCapabilitiesResponse{
			Capabilities: []*csi.NodeServiceCapability{
				{
					Type: &csi.NodeServiceCapability_Rpc{
						Rpc: &csi.NodeServiceCapability_RPC{
							Type: csi.NodeServiceCapability_RPC_STAGE_UNSTAGE_VOLUME,
						},
					},
				},
			},
		}, nil
	}
	return &csi.NodeGetCapabilitiesResponse{}, nil
}
