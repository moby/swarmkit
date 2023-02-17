package plugin

import (
	"context"
	"net"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/testutils"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"google.golang.org/grpc/codes"
)

func newVolumeClient(name string, nodeID string) *nodePlugin {
	p := &testutils.FakeCompatPlugin{
		PluginName: name,
		PluginAddr: &net.UnixAddr{},
	}
	n := newNodePlugin(name, p, p, nil)
	n.staging = true

	fakeNodeClient := newFakeNodeClient(true, nodeID)
	n.nodeClient = fakeNodeClient
	return n
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
			AccessType: &api.VolumeAccessMode_Mount{
				Mount: &api.VolumeAccessMode_MountVolume{},
			},
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
			AccessType: &api.VolumeAccessMode_Mount{
				Mount: &api.VolumeAccessMode_MountVolume{},
			},
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
	var testcases = []struct {
		staging bool
		plugin  string
	}{
		{
			plugin:  "staging-plugin",
			staging: true,
		},
		{
			plugin:  "non-staging-plugin",
			staging: false,
		},
	}

	for _, tc := range testcases {
		nodePlugin := newVolumeClient(tc.plugin, "node-1")
		s := &api.VolumeAssignment{
			VolumeID: "vol3",
			AccessMode: &api.VolumeAccessMode{
				Scope:   api.VolumeScopeMultiNode,
				Sharing: api.VolumeSharingOneWriter,
				AccessType: &api.VolumeAccessMode_Mount{
					Mount: &api.VolumeAccessMode_MountVolume{},
				},
			},
			Driver: &api.Driver{
				Name: tc.plugin,
			},
		}

		nodePlugin.staging = tc.staging
		if nodePlugin.staging {
			err := nodePlugin.NodePublishVolume(context.Background(), s)
			assert.Equal(t, codes.FailedPrecondition, testutils.ErrorCode(err))

			// Volume needs to be staged before publishing. Stage -> Publish
			err = nodePlugin.NodeStageVolume(context.Background(), s)
			require.NoError(t, err)
		}
		err := nodePlugin.NodePublishVolume(context.Background(), s)
		require.NoError(t, err)
	}
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
			AccessType: &api.VolumeAccessMode_Mount{
				Mount: &api.VolumeAccessMode_MountVolume{},
			},
		},
		Driver: &api.Driver{
			Name: plugin,
		},
	}
	// don't need to publish volume, necessarily, to unpublish itt.

	// Volume needs to be staged and published before unpublish. Stage -> Publish -> Unpublish
	err := nodePlugin.NodeStageVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodePublishVolume(context.Background(), s)
	require.NoError(t, err)
	err = nodePlugin.NodeUnpublishVolume(context.Background(), s)
	require.NoError(t, err)
}
