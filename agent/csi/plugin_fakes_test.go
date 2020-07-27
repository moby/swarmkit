package csi

import (
	"context"
	"fmt"

	"google.golang.org/grpc"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

type fakeNodeClient struct {
	// getInfoRequests is a log of all requests to NodeGetInfo.
	getInfoRequests []*csi.NodeGetInfoRequest
	// idCounter is a simple way to generate ids
	idCounter int
}

func newFakeNodeClient() *fakeNodeClient {
	return &fakeNodeClient{
		getInfoRequests: []*csi.NodeGetInfoRequest{},
	}
}

func (f *fakeNodeClient) NodeGetInfo(ctx context.Context, in *csi.NodeGetInfoRequest, _ ...grpc.CallOption) (*csi.NodeGetInfoResponse, error) {
	f.idCounter++
	f.getInfoRequests = append(f.getInfoRequests, in)
	return &csi.NodeGetInfoResponse{
		NodeId: fmt.Sprintf("nodeid%d", f.idCounter),
	}, nil
}

func (f *fakeNodeClient) NodeStageVolume(ctx context.Context, in *csi.NodeStageVolumeRequest, opts ...grpc.CallOption) (*csi.NodeStageVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeUnstageVolume(ctx context.Context, in *csi.NodeUnstageVolumeRequest, opts ...grpc.CallOption) (*csi.NodeUnstageVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodePublishVolume(ctx context.Context, in *csi.NodePublishVolumeRequest, opts ...grpc.CallOption) (*csi.NodePublishVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeUnpublishVolume(ctx context.Context, in *csi.NodeUnpublishVolumeRequest, opts ...grpc.CallOption) (*csi.NodeUnpublishVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeGetVolumeStats(ctx context.Context, in *csi.NodeGetVolumeStatsRequest, opts ...grpc.CallOption) (*csi.NodeGetVolumeStatsResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeExpandVolume(ctx context.Context, in *csi.NodeExpandVolumeRequest, opts ...grpc.CallOption) (*csi.NodeExpandVolumeResponse, error) {
	return nil, nil
}

func (f *fakeNodeClient) NodeGetCapabilities(ctx context.Context, in *csi.NodeGetCapabilitiesRequest, opts ...grpc.CallOption) (*csi.NodeGetCapabilitiesResponse, error) {
	return nil, nil
}
