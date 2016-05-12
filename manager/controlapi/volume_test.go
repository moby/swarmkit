package controlapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createVolumeSpec(name string) *api.VolumeSpec {
	return &api.VolumeSpec{
		Annotations: api.Annotations{
			Name: name,
		},
	}
}

func createVolume(t *testing.T, ts *testServer, name string) *api.Volume {
	spec := createVolumeSpec(name)
	r, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{Spec: spec})
	assert.NoError(t, err)
	return r.Volume
}

func TestValidateVolumeSpec(t *testing.T) {
	err := validateVolumeSpec(nil)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}

func TestCreateVolume(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	spec := createVolumeSpec("name")
	r, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{Spec: spec})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Volume.ID)
}

func TestGetVolume(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetVolume(context.Background(), &api.GetVolumeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetVolume(context.Background(), &api.GetVolumeRequest{VolumeID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	volume := createVolume(t, ts, "name")
	r, err := ts.Client.GetVolume(context.Background(), &api.GetVolumeRequest{VolumeID: volume.ID})
	assert.NoError(t, err)
	volume.Meta.Version = r.Volume.Meta.Version
	assert.Equal(t, volume, r.Volume)
}

func TestRemoveVolume(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	volume := createVolume(t, ts, "name")
	r, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{VolumeID: volume.ID})
	assert.NoError(t, err)
	assert.NotNil(t, r)
}

func TestListVolumes(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListVolumes(context.Background(), &api.ListVolumesRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Volumes)

	createVolume(t, ts, "name1")
	r, err = ts.Client.ListVolumes(context.Background(), &api.ListVolumesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Volumes))

	createVolume(t, ts, "name2")
	createVolume(t, ts, "name3")
	r, err = ts.Client.ListVolumes(context.Background(), &api.ListVolumesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Volumes))
}
