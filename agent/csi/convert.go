package csi

import (
	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

// makeNodeInfo converts a csi.NodeGetInfoResponse object into a swarmkit NodeCSIInfo
// object.
func makeNodeInfo(csiNodeInfo *csi.NodeGetInfoResponse) *api.NodeCSIInfo {
	return &api.NodeCSIInfo{
		NodeID:            csiNodeInfo.NodeId,
		MaxVolumesPerNode: csiNodeInfo.MaxVolumesPerNode,
	}
}
