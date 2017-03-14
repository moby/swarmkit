package storeapi

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestGetObject(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	_, err := ts.Client.GetObject(ctx, &api.GetObjectRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetObject(ctx, &api.GetObjectRequest{Kind: "node"})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetObject(ctx, &api.GetObjectRequest{Kind: "node", ObjectID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	node := createNode(t, ts, "id", api.NodeRoleManager, api.NodeMembershipAccepted, api.NodeStatus_READY)
	r, err := ts.Client.GetObject(ctx, &api.GetObjectRequest{Kind: "node", ObjectID: node.ID})
	assert.NoError(t, err)
	assert.Equal(t, node, r.GetNode())
}
