package service

import (
	"path"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"golang.org/x/net/context"

	"github.com/container-storage-interface/spec/lib/go/csi"
)

func (s *service) NodeStageVolume(
	ctx context.Context,
	req *csi.NodeStageVolumeRequest) (
	*csi.NodeStageVolumeResponse, error) {

	return nil, status.Error(codes.Unimplemented, "")
}

func (s *service) NodeUnstageVolume(
	ctx context.Context,
	req *csi.NodeUnstageVolumeRequest) (
	*csi.NodeUnstageVolumeResponse, error) {

	return nil, status.Error(codes.Unimplemented, "")
}

func (s *service) NodePublishVolume(
	ctx context.Context,
	req *csi.NodePublishVolumeRequest) (
	*csi.NodePublishVolumeResponse, error) {

	device, ok := req.PublishContext["device"]
	if !ok {
		return nil, status.Error(
			codes.InvalidArgument,
			"publish volume info 'device' key required")
	}

	s.volsRWL.Lock()
	defer s.volsRWL.Unlock()

	i, v := s.findVolNoLock("id", req.VolumeId)
	if i < 0 {
		return nil, status.Error(codes.NotFound, req.VolumeId)
	}

	// nodeMntPathKey is the key in the volume's attributes that is set to a
	// mock mount path if the volume has been published by the node
	nodeMntPathKey := path.Join(s.nodeID, req.TargetPath)

	// Check to see if the volume has already been published.
	if v.VolumeContext[nodeMntPathKey] != "" {

		// Requests marked Readonly fail due to volumes published by
		// the Mock driver supporting only RW mode.
		if req.Readonly {
			return nil, status.Error(codes.AlreadyExists, req.VolumeId)
		}

		return &csi.NodePublishVolumeResponse{}, nil
	}

	// Publish the volume.
	v.VolumeContext[nodeMntPathKey] = device
	s.vols[i] = v

	return &csi.NodePublishVolumeResponse{}, nil
}

func (s *service) NodeUnpublishVolume(
	ctx context.Context,
	req *csi.NodeUnpublishVolumeRequest) (
	*csi.NodeUnpublishVolumeResponse, error) {

	s.volsRWL.Lock()
	defer s.volsRWL.Unlock()

	i, v := s.findVolNoLock("id", req.VolumeId)
	if i < 0 {
		return nil, status.Error(codes.NotFound, req.VolumeId)
	}

	// nodeMntPathKey is the key in the volume's attributes that is set to a
	// mock mount path if the volume has been published by the node
	nodeMntPathKey := path.Join(s.nodeID, req.TargetPath)

	// Check to see if the volume has already been unpublished.
	if v.VolumeContext[nodeMntPathKey] == "" {
		return &csi.NodeUnpublishVolumeResponse{}, nil
	}

	// Unpublish the volume.
	delete(v.VolumeContext, nodeMntPathKey)
	s.vols[i] = v

	return &csi.NodeUnpublishVolumeResponse{}, nil
}

func (s *service) NodeGetInfo(
	ctx context.Context,
	req *csi.NodeGetInfoRequest) (
	*csi.NodeGetInfoResponse, error) {

	return &csi.NodeGetInfoResponse{
		NodeId: s.nodeID,
	}, nil
}

func (s *service) NodeGetCapabilities(
	ctx context.Context,
	req *csi.NodeGetCapabilitiesRequest) (
	*csi.NodeGetCapabilitiesResponse, error) {

	return &csi.NodeGetCapabilitiesResponse{}, nil
}

func (s *service) NodeGetVolumeStats(
	ctx context.Context,
	req *csi.NodeGetVolumeStatsRequest) (
	*csi.NodeGetVolumeStatsResponse, error) {

	var f *csi.Volume
	for _, v := range s.vols {
		if v.VolumeId == req.VolumeId {
			f = &v
		}
	}
	if f == nil {
		return nil, status.Errorf(codes.NotFound, "No volume found with id %s", req.VolumeId)
	}

	return &csi.NodeGetVolumeStatsResponse{
		Usage: []*csi.VolumeUsage{
			&csi.VolumeUsage{
				Available: int64(float64(f.CapacityBytes) * 0.6),
				Total:     f.CapacityBytes,
				Used:      int64(float64(f.CapacityBytes) * 0.4),
				Unit:      csi.VolumeUsage_BYTES,
			},
		},
	}, nil
}

func (s *service) NodeExpandVolume(
	ctx context.Context,
	req *csi.NodeExpandVolumeRequest) (
	*csi.NodeExpandVolumeResponse, error) {

	return nil, status.Error(codes.Unimplemented, "")
}
