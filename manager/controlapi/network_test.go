package controlapi

import (
	"context"
	"testing"

	"github.com/moby/swarmkit/v2/testutils"

	"google.golang.org/grpc/codes"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createNetworkSpec(name string) *api.NetworkSpec {
	return &api.NetworkSpec{
		Annotations: api.Annotations{
			Name: name,
		},
	}
}

// createInternalNetwork creates an internal network for testing. it is the same
// as Server.CreateNetwork except without the label check.
func (s *Server) createInternalNetwork(ctx context.Context, request *api.CreateNetworkRequest) (*api.CreateNetworkResponse, error) {
	if err := s.validateNetworkSpec(request.Spec); err != nil {
		return nil, err
	}

	// TODO(mrjana): Consider using `Name` as a primary key to handle
	// duplicate creations. See #65
	n := &api.Network{
		ID:   identity.NewID(),
		Spec: *request.Spec,
	}

	err := s.store.Update(func(tx store.Tx) error {
		return store.CreateNetwork(tx, n)
	})
	if err != nil {
		return nil, err
	}

	return &api.CreateNetworkResponse{
		Network: n,
	}, nil
}

func createServiceInNetworkSpec(name, image string, nwid string, instances uint64) *api.ServiceSpec {
	return &api.ServiceSpec{
		Annotations: api.Annotations{
			Name: name,
			Labels: map[string]string{
				"common": "yes",
				"unique": name,
			},
		},
		Task: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: image,
				},
			},
		},
		Mode: &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: instances,
			},
		},
		Networks: []*api.NetworkAttachmentConfig{
			{
				Target: nwid,
			},
		},
	}
}

func createServiceInNetwork(t *testing.T, ts *testServer, name, image string, nwid string, instances uint64) *api.Service {
	spec := createServiceInNetworkSpec(name, image, nwid, instances)
	r, err := ts.Client.CreateService(context.Background(), &api.CreateServiceRequest{Spec: spec})
	require.NoError(t, err)
	return r.Service
}

func TestValidateIPAMConfiguration(t *testing.T) {
	err := validateIPAMConfiguration(nil)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf := &api.IPAMConfig{
		Subnet: "",
	}

	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Subnet = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Subnet = "192.168.0.0/16"
	err = validateIPAMConfiguration(IPAMConf)
	require.NoError(t, err)

	IPAMConf.Range = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Range = "192.169.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Range = "192.168.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	require.NoError(t, err)

	IPAMConf.Gateway = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Gateway = "192.169.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	IPAMConf.Gateway = "192.168.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	require.NoError(t, err)
}

func TestCreateNetwork(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet1"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr.Network)
	assert.NotEmpty(t, nr.Network.ID)
}

func TestGetNetwork(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet2"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr.Network)
	assert.NotEmpty(t, nr.Network.ID)

	_, err = ts.Client.GetNetwork(context.Background(), &api.GetNetworkRequest{NetworkID: nr.Network.ID})
	require.NoError(t, err)
}

func TestRemoveNetwork(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet3"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr.Network)
	assert.NotEmpty(t, nr.Network.ID)

	_, err = ts.Client.RemoveNetwork(context.Background(), &api.RemoveNetworkRequest{NetworkID: nr.Network.ID})
	require.NoError(t, err)
}

func TestRemoveNetworkWithAttachedService(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet4"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr.Network)
	assert.NotEmpty(t, nr.Network.ID)
	createServiceInNetwork(t, ts, "name", "image", nr.Network.ID, 1)
	_, err = ts.Client.RemoveNetwork(context.Background(), &api.RemoveNetworkRequest{NetworkID: nr.Network.ID})
	require.Error(t, err)
}

func TestCreateNetworkInvalidDriver(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	spec := createNetworkSpec("baddrivernet")
	spec.DriverConfig = &api.Driver{
		Name: "invalid-must-never-exist",
	}
	_, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: spec,
	})
	require.Error(t, err)
}

func TestListNetworks(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	nr1, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("listtestnet1"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr1.Network)
	assert.NotEmpty(t, nr1.Network.ID)

	nr2, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("listtestnet2"),
	})
	require.NoError(t, err)
	assert.NotNil(t, nr2.Network)
	assert.NotEmpty(t, nr2.Network.ID)

	r, err := ts.Client.ListNetworks(context.Background(), &api.ListNetworksRequest{})
	require.NoError(t, err)
	assert.Len(t, r.Networks, 3) // Account ingress network
	for _, nw := range r.Networks {
		if nw.Spec.Ingress {
			continue
		}
		assert.True(t, nw.ID == nr1.Network.ID || nw.ID == nr2.Network.ID)
	}
}
