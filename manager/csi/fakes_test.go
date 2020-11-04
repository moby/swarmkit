package csi

import (
	"context"
	"fmt"
	"sync"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/golang/protobuf/ptypes/wrappers"
	"google.golang.org/grpc"

	"github.com/docker/swarmkit/api"
)

// failDeleteLabel is a label set on a Volume to cause DeleteVolume on the fake
// plugin to fail
const failDeleteLabel = "fakes_fail_delete"

// volumes_fakes_test.go includes the fakes for unit-testing parts of the
// volumes code.

// fakeSecretProvider implements the SecretProvider interface.
type fakeSecretProvider struct {
	secretMap map[string]*api.Secret
}

func (f *fakeSecretProvider) GetSecret(id string) *api.Secret {
	return f.secretMap[id]
}

// fakeIdentityClient implements the csi IdentityClient interface.
type fakeIdentityClient struct {
	caps []*csi.PluginCapability
}

// newFakeIdentityClient creates a new fake identity client which returns a
// default set of capabilities. create a fakeIdentityClient by hand for
// different capabilities
func newFakeIdentityClient() *fakeIdentityClient {
	return &fakeIdentityClient{
		caps: []*csi.PluginCapability{
			{
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_CONTROLLER_SERVICE,
					},
				},
			}, {
				Type: &csi.PluginCapability_Service_{
					Service: &csi.PluginCapability_Service{
						Type: csi.PluginCapability_Service_VOLUME_ACCESSIBILITY_CONSTRAINTS,
					},
				},
			},
		},
	}
}

func (f *fakeIdentityClient) GetPluginInfo(ctx context.Context, _ *csi.GetPluginInfoRequest, _ ...grpc.CallOption) (*csi.GetPluginInfoResponse, error) {
	return &csi.GetPluginInfoResponse{Name: "plugin", VendorVersion: "1"}, nil
}

func (f *fakeIdentityClient) GetPluginCapabilities(ctx context.Context, _ *csi.GetPluginCapabilitiesRequest, _ ...grpc.CallOption) (*csi.GetPluginCapabilitiesResponse, error) {
	return &csi.GetPluginCapabilitiesResponse{
		Capabilities: f.caps,
	}, nil
}

func (f *fakeIdentityClient) Probe(ctx context.Context, in *csi.ProbeRequest, _ ...grpc.CallOption) (*csi.ProbeResponse, error) {
	return &csi.ProbeResponse{
		Ready: &wrappers.BoolValue{
			Value: true,
		},
	}, nil
}

type fakeControllerClient struct {
	volumes    map[string]*csi.Volume
	namesToIds map[string]string
	// createVolumeRequests is a log of all requests to CreateVolume.
	createVolumeRequests []*csi.CreateVolumeRequest
	// publishRequests is a log of all requests to ControllerPublishVolume
	publishRequests []*csi.ControllerPublishVolumeRequest
	// idCounter is a simple way to generate ids
	idCounter int
}

func newFakeControllerClient() *fakeControllerClient {
	return &fakeControllerClient{
		volumes:              map[string]*csi.Volume{},
		namesToIds:           map[string]string{},
		createVolumeRequests: []*csi.CreateVolumeRequest{},
		publishRequests:      []*csi.ControllerPublishVolumeRequest{},
	}
}

func (f *fakeControllerClient) CreateVolume(ctx context.Context, in *csi.CreateVolumeRequest, _ ...grpc.CallOption) (*csi.CreateVolumeResponse, error) {
	f.idCounter++
	f.createVolumeRequests = append(f.createVolumeRequests, in)

	var topology []*csi.Topology
	if in.AccessibilityRequirements != nil {
		topology = in.AccessibilityRequirements.Requisite
	}

	return &csi.CreateVolumeResponse{
		Volume: &csi.Volume{
			VolumeId: fmt.Sprintf("volumeid%d", f.idCounter),
			VolumeContext: map[string]string{
				"someFlag":      "yeah",
				"requestNumber": fmt.Sprintf("%d", f.idCounter),
			},
			CapacityBytes:      1000000000,
			AccessibleTopology: topology,
		},
	}, nil
}

func (f *fakeControllerClient) DeleteVolume(ctx context.Context, in *csi.DeleteVolumeRequest, _ ...grpc.CallOption) (*csi.DeleteVolumeResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ControllerPublishVolume(ctx context.Context, in *csi.ControllerPublishVolumeRequest, _ ...grpc.CallOption) (*csi.ControllerPublishVolumeResponse, error) {
	// first, add the publish request to the slice of requests.
	f.publishRequests = append(f.publishRequests, in)
	return &csi.ControllerPublishVolumeResponse{
		PublishContext: map[string]string{
			"bruh": "dude",
		},
	}, nil
}

func (f *fakeControllerClient) ControllerUnpublishVolume(ctx context.Context, in *csi.ControllerUnpublishVolumeRequest, _ ...grpc.CallOption) (*csi.ControllerUnpublishVolumeResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ValidateVolumeCapabilities(ctx context.Context, in *csi.ValidateVolumeCapabilitiesRequest, _ ...grpc.CallOption) (*csi.ValidateVolumeCapabilitiesResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ListVolumes(ctx context.Context, in *csi.ListVolumesRequest, _ ...grpc.CallOption) (*csi.ListVolumesResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) GetCapacity(ctx context.Context, in *csi.GetCapacityRequest, _ ...grpc.CallOption) (*csi.GetCapacityResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ControllerGetCapabilities(ctx context.Context, in *csi.ControllerGetCapabilitiesRequest, _ ...grpc.CallOption) (*csi.ControllerGetCapabilitiesResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) CreateSnapshot(ctx context.Context, in *csi.CreateSnapshotRequest, _ ...grpc.CallOption) (*csi.CreateSnapshotResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) DeleteSnapshot(ctx context.Context, in *csi.DeleteSnapshotRequest, _ ...grpc.CallOption) (*csi.DeleteSnapshotResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ListSnapshots(ctx context.Context, in *csi.ListSnapshotsRequest, _ ...grpc.CallOption) (*csi.ListSnapshotsResponse, error) {
	return nil, nil
}

func (f *fakeControllerClient) ControllerExpandVolume(ctx context.Context, in *csi.ControllerExpandVolumeRequest, _ ...grpc.CallOption) (*csi.ControllerExpandVolumeResponse, error) {
	return nil, nil
}

// fakePluginMaker keeps track of which plugins have been created
type fakePluginMaker struct {
	sync.Mutex
	plugins map[string]*fakePlugin
}

func (fpm *fakePluginMaker) newFakePlugin(config *api.CSIConfig_Plugin, provider SecretProvider) Plugin {
	fpm.Lock()
	defer fpm.Unlock()
	p := &fakePlugin{
		name:             config.Name,
		socket:           config.ControllerSocket,
		swarmToCSI:       map[string]string{},
		volumesCreated:   map[string]*api.Volume{},
		volumesDeleted:   []string{},
		volumesPublished: map[string][]string{},
		removedIDs:       map[string]struct{}{},
	}
	fpm.plugins[config.Name] = p
	return p
}

type fakePlugin struct {
	name       string
	socket     string
	swarmToCSI map[string]string
	// removedIDs is a set of node IDs for which RemoveNode has been called.
	removedIDs map[string]struct{}

	volumesCreated map[string]*api.Volume
	// volumesDelete is the list of volumes for which DeleteVolume has been
	// called
	volumesDeleted []string
	// volumesPublished maps the ID of a Volume to the Nodes it was published
	// to
	volumesPublished map[string][]string
}

func (f *fakePlugin) CreateVolume(ctx context.Context, v *api.Volume) (*api.VolumeInfo, error) {
	f.volumesCreated[v.ID] = v
	return &api.VolumeInfo{
		VolumeID: fmt.Sprintf("csi_%v", v.ID),
		VolumeContext: map[string]string{
			"exists": "yes",
		},
	}, nil
}

func (f *fakePlugin) DeleteVolume(ctx context.Context, v *api.Volume) error {
	// always append the volume ID, even if we fail, so we know this was called
	f.volumesDeleted = append(f.volumesDeleted, v.ID)
	if msg, ok := v.Spec.Annotations.Labels[failDeleteLabel]; ok {
		return fmt.Errorf("failing delete: %s", msg)
	}
	return nil
}

func (f *fakePlugin) PublishVolume(ctx context.Context, v *api.Volume, nodeID string) (map[string]string, error) {
	f.volumesPublished[v.ID] = append(f.volumesPublished[v.ID], nodeID)
	// TODO(dperny): return somethign here
	return map[string]string{
		"faked": "yeah",
	}, nil
}

func (f *fakePlugin) AddNode(swarmID, csiID string) {
	f.swarmToCSI[swarmID] = csiID
}

func (f *fakePlugin) RemoveNode(swarmID string) {
	f.removedIDs[swarmID] = struct{}{}
}
