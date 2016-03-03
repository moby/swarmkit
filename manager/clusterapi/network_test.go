package clusterapi

import (
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestCreateNetwork(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.CreateNetwork(context.Background(), &api.CreateNetworkRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestGetNetwork(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.GetNetwork(context.Background(), &api.GetNetworkRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestDeleteNetwork(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.DeleteNetwork(context.Background(), &api.DeleteNetworkRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}

func TestListNetworks(t *testing.T) {
	ts := newTestServer(t)
	_, err := ts.Client.ListNetworks(context.Background(), &api.ListNetworksRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.Unimplemented, grpc.Code(err))
}
