package clusterapi

import (
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func createNode(t *testing.T, ts *testServer, id string) *api.Node {
	node := &api.Node{
		ID: id,
	}
	err := ts.Store.Update(func(tx state.Tx) error {
		return tx.Nodes().Create(node)
	})
	assert.NoError(t, err)
	return node
}

func TestGetNode(t *testing.T) {
	ts := newTestServer(t)

	_, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	node := createNode(t, ts, "id")
	r, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: node.ID})
	assert.NoError(t, err)
	assert.Equal(t, node.ID, r.Node.ID)
}

func TestListNodes(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Nodes)

	createNode(t, ts, "id1")
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Nodes))

	createNode(t, ts, "id2")
	createNode(t, ts, "id3")
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Nodes))
}

func TestUpdateNode(t *testing.T) {
	ts := newTestServer(t)

	_, err := ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "invalid", Spec: &api.NodeSpec{}, NodeVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
		NodeVersion: &api.Version{},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	createNode(t, ts, "id")
	r, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "id"})
	assert.NoError(t, err)
	assert.NotNil(t, r.Node)
	assert.Nil(t, r.Node.Spec)

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id"})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
		NodeVersion: &r.Node.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "id"})
	assert.NoError(t, err)
	assert.NotNil(t, r.Node)
	assert.NotNil(t, r.Node.Spec)
	assert.Equal(t, api.NodeAvailabilityDrain, r.Node.Spec.Availability)

	version := &r.Node.Version
	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id", Spec: r.Node.Spec, NodeVersion: version})
	assert.NoError(t, err)
	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id", Spec: r.Node.Spec, NodeVersion: version})
	assert.Error(t, err)
}
