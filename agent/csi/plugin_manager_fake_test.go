package csi

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/moby/swarmkit/v2/agent/csi/plugin"
	"github.com/moby/swarmkit/v2/api"
)

// fakePluginManager is a fake pluginManager, used for testing the volume
// manager.
type fakePluginManager struct {
	plugins map[string]*fakeNodePlugin
}

func newFakePluginManager() *fakePluginManager {
	return &fakePluginManager{
		plugins: map[string]*fakeNodePlugin{},
	}
}

// Get returns the plugin with the given name.
func (f *fakePluginManager) Get(name string) (plugin.NodePlugin, error) {
	if plugin, ok := f.plugins[name]; ok {
		return plugin, nil
	}
	return nil, fmt.Errorf("error getting plugin")
}

// NodeInfo returns the CSI Info for every plugin.
func (f *fakePluginManager) NodeInfo(ctx context.Context) ([]*api.NodeCSIInfo, error) {
	return nil, nil
}

// fakeNodePlugin is a fake plugin.NodePlugin, used for testing the CSI Volume
// Manager.
type fakeNodePlugin struct {
	// stagedVolumes is the set of all VolumeAssignments successfully staged.
	stagedVolumes map[string]*api.VolumeAssignment
	// publishedVolumes is the map from the volumeID to the publish path.
	publishedVolumes map[string]string
}

func newFakeNodePlugin(name string) *fakeNodePlugin {
	return &fakeNodePlugin{
		stagedVolumes:    map[string]*api.VolumeAssignment{},
		publishedVolumes: map[string]string{},
	}
}

// NodeGetInfo is a blank stub, because it is not needed to test the CSI Volume
// Manager.
func (f *fakeNodePlugin) NodeGetInfo(ctx context.Context) (*api.NodeCSIInfo, error) {
	return nil, nil
}

func (f *fakeNodePlugin) GetPublishedPath(volumeID string) string {
	return f.publishedVolumes[volumeID]
}

func (f *fakeNodePlugin) NodeStageVolume(ctx context.Context, req *api.VolumeAssignment) error {
	f.stagedVolumes[req.ID] = req
	return nil
}

func (f *fakeNodePlugin) NodeUnstageVolume(ctx context.Context, req *api.VolumeAssignment) error {
	if _, ok := f.stagedVolumes[req.ID]; !ok {
		return status.Error(codes.FailedPrecondition, "volume not staged")
	}
	return nil
}

func (f *fakeNodePlugin) NodePublishVolume(ctx context.Context, req *api.VolumeAssignment) error {
	if _, ok := f.stagedVolumes[req.ID]; !ok {
		return status.Error(codes.FailedPrecondition, "volume not staged")
	}
	// the path here isn't important, because the path is an implementation
	// detail. it just needs to be non-empty.
	f.publishedVolumes[req.ID] = fmt.Sprintf("path_%s", req.ID)
	return nil
}

func (f *fakeNodePlugin) NodeUnpublishVolume(ctx context.Context, req *api.VolumeAssignment) error {
	if _, ok := f.publishedVolumes[req.ID]; !ok {
		return status.Error(codes.FailedPrecondition, "volume not published")
	}

	return nil
}
