package plugin

import (
	"context"
	"fmt"

	"github.com/docker/docker/pkg/plugingetter"

	"github.com/moby/swarmkit/v2/api"
)

// plugin_fake_test.go contains code for faking node plugins in the context of
// testing the plugin manager. A different fake should be used for testing the
// volume manager, which is in a different package.

type fakeNodePlugin struct {
	name   string
	socket string
}

// newFakeNodePlugin has the same signature as NewNodePlugin, allowing it to be
// substituted in testing.
func newFakeNodePlugin(name string, pc plugingetter.CompatPlugin, pa plugingetter.PluginAddr, secrets SecretGetter) NodePlugin {
	return &fakeNodePlugin{
		name:   name,
		socket: pa.Addr().String(),
	}
}

// NodeGetInfo returns a canned NodeCSIInfo request for the plugin.
func (f *fakeNodePlugin) NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error) {
	if f.socket == "fail" {
		return nil, fmt.Errorf("plugin %s is not ready", f.name)
	}
	return &api.NodeCSIInfo{
		PluginName: f.name,
		NodeID:     fmt.Sprintf("node_%s", f.name),
	}, nil
}

// these methods are all stubs, as they are not needed for testing the
// Manager.
func (f *fakeNodePlugin) GetPublishedPath(volumeID string) string {
	return ""
}

func (f *fakeNodePlugin) NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error {
	return nil
}

func (f *fakeNodePlugin) NodeUnstageVolume(ctx context.Context, req *api.VolumeAssignment) error {
	return nil
}

func (f *fakeNodePlugin) NodePublishVolume(ctx context.Context, req *api.VolumeAssignment) error {
	return nil
}

func (f *fakeNodePlugin) NodeUnpublishVolume(ctx context.Context, req *api.VolumeAssignment) error {
	return nil
}
