package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/pb/docker/cluster/api"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createNetworkSpec(name string) *specspb.NetworkSpec {
	return &specspb.NetworkSpec{
		Meta: specspb.Meta{
			Name: name,
		},
	}
}

func TestValidateDriver(t *testing.T) {
	assert.NoError(t, validateDriver(nil))

	err := validateDriver(&typespb.Driver{Name: ""})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}

func TestValidateIPAMConfiguration(t *testing.T) {
	err := validateIPAMConfiguration(nil)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf := &typespb.IPAMConfiguration{
		Subnet: "",
	}

	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Subnet = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Subnet = "192.168.0.0/16"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)

	IPAMConf.Range = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Range = "192.169.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Range = "192.168.1.0/24"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)

	IPAMConf.Gateway = "bad"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Gateway = "192.169.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	IPAMConf.Gateway = "192.168.1.1"
	err = validateIPAMConfiguration(IPAMConf)
	assert.NoError(t, err)
}

func TestValidateIPAM(t *testing.T) {
	assert.NoError(t, validateIPAM(nil))
}

func TestValidateNetworkSpec(t *testing.T) {
	err := validateNetworkSpec(nil)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))
}

func TestCreateNetwork(t *testing.T) {
	ts := newTestServer(t)
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet1"),
	})
	assert.NoError(t, err)
	assert.NotEqual(t, nr.Network, nil)
	assert.NotEqual(t, nr.Network.ID, "")
}

func TestGetNetwork(t *testing.T) {
	ts := newTestServer(t)
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet2"),
	})
	assert.NoError(t, err)
	assert.NotEqual(t, nr.Network, nil)
	assert.NotEqual(t, nr.Network.ID, "")

	_, err = ts.Client.GetNetwork(context.Background(), &api.GetNetworkRequest{NetworkID: nr.Network.ID})
	assert.NoError(t, err)
}

func TestRemoveNetwork(t *testing.T) {
	ts := newTestServer(t)
	nr, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("testnet2"),
	})
	assert.NoError(t, err)
	assert.NotEqual(t, nr.Network, nil)
	assert.NotEqual(t, nr.Network.ID, "")

	_, err = ts.Client.RemoveNetwork(context.Background(), &api.RemoveNetworkRequest{NetworkID: nr.Network.ID})
	assert.NoError(t, err)
}

func TestListNetworks(t *testing.T) {
	ts := newTestServer(t)

	nr1, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("listtestnet1"),
	})
	assert.NoError(t, err)
	assert.NotEqual(t, nr1.Network, nil)
	assert.NotEqual(t, nr1.Network.ID, "")

	nr2, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{
		Spec: createNetworkSpec("listtestnet2"),
	})
	assert.NoError(t, err)
	assert.NotEqual(t, nr2.Network, nil)
	assert.NotEqual(t, nr2.Network.ID, "")

	r, err := ts.Client.ListNetworks(context.Background(), &api.ListNetworksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Networks))
	assert.True(t, r.Networks[0].ID == nr1.Network.ID || r.Networks[0].ID == nr2.Network.ID)
	assert.True(t, r.Networks[1].ID == nr1.Network.ID || r.Networks[1].ID == nr2.Network.ID)
}
