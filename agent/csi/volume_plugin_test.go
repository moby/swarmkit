package csi

import (
	"context"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
)

func newVolumeClient(name string, nodeID string) *NodePlugin {
	return NewNodePlugin(name, nodeID)
}

func TestNodeStageVolume(t *testing.T) {
	plugin := "plugin-1"
	node := "node-1"
	nodePlugin := newVolumeClient(plugin, node)
	s := &api.VolumeAssignment{
		VolumeID: "vol1",
		AccessMode: &api.VolumeAccessMode{
			Scope:   api.VolumeScopeMultiNode,
			Sharing: api.VolumeSharingOneWriter,
		},
		Driver: &api.Driver{
			Name: plugin,
		},
	}
	err := nodePlugin.NodeStageVolume(context.Background(), s)
	require.NoError(t, err)
}

func TestNodeUnstageVolume(t *testing.T) {
	plugin := "plugin-2"
	node := "node-1"
	nodePlugin := newVolumeClient(plugin, node)
	s := &api.VolumeAssignment{
		VolumeID: "vol2",
		AccessMode: &api.VolumeAccessMode{
			Scope:   api.VolumeScopeMultiNode,
			Sharing: api.VolumeSharingOneWriter,
		},
		Driver: &api.Driver{
			Name: plugin,
		},
	}
	err := nodePlugin.NodeUnstageVolume(context.Background(), s)
	assert.Equal(t, codes.FailedPrecondition, testutils.ErrorCode(err))

	// Voluume needs to be unpublished before unstaging. Stage -> Publish -> Unpublish -> Unstage
	err = nodePlugin.NodeStageVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodePublishVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodeUnpublishVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodeUnstageVolume(context.Background(), s)
	require.NoError(t, err)
}

func TestNodePublishVolume(t *testing.T) {
	plugin := "plugin-3"
	node := "node-1"
	nodePlugin := newVolumeClient(plugin, node)
	s := &api.VolumeAssignment{
		VolumeID: "vol3",
		AccessMode: &api.VolumeAccessMode{
			Scope:   api.VolumeScopeMultiNode,
			Sharing: api.VolumeSharingOneWriter,
		},
		Driver: &api.Driver{
			Name: plugin,
		},
	}
	err := nodePlugin.NodePublishVolume(context.Background(), s)
	assert.Equal(t, codes.FailedPrecondition, testutils.ErrorCode(err))

	// Volume needs to be staged before publishing. Stage -> Publish
	err = nodePlugin.NodeStageVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodePublishVolume(context.Background(), s)
	require.NoError(t, err)
}

func TestNodeUnpublishVolume(t *testing.T) {
	plugin := "plugin-4"
	node := "node-1"
	nodePlugin := newVolumeClient(plugin, node)
	s := &api.VolumeAssignment{
		VolumeID: "vol4",
		AccessMode: &api.VolumeAccessMode{
			Scope:   api.VolumeScopeMultiNode,
			Sharing: api.VolumeSharingOneWriter,
		},
		Driver: &api.Driver{
			Name: plugin,
		},
	}
	err := nodePlugin.NodeUnpublishVolume(context.Background(), s)
	assert.Equal(t, codes.FailedPrecondition, testutils.ErrorCode(err))

	// Volume needs to be staged and published before unpublish. Stage -> Publish -> Unpublish
	err = nodePlugin.NodeStageVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodePublishVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodeUnpublishVolume(context.Background(), s)
	require.NoError(t, err)
}
