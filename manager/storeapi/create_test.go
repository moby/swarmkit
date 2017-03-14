package storeapi

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestCreateObject(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	objEmpty := &api.Object{}
	objHasID := &api.Object{Object: &api.Object_Node{Node: &api.Node{ID: "foo"}}}
	objValid := &api.Object{Object: &api.Object_Node{Node: &api.Node{
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipAccepted,
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRoleManager,
	}}}

	_, err := ts.Client.CreateObject(ctx, objEmpty)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.CreateObject(ctx, objHasID)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	returnedObj, err := ts.Client.CreateObject(ctx, objValid)
	assert.NoError(t, err)
	assert.Equal(t, returnedObj.GetNode().Spec, objValid.GetNode().Spec)
	var storeNode *api.Node
	ts.Store.View(func(readTx store.ReadTx) {
		storeNode = store.GetNode(readTx, returnedObj.GetNode().ID)
	})
	assert.Equal(t, storeNode, returnedObj.GetNode())
	assert.Equal(t, uint64(0), returnedObj.GetNode().Meta.Version.Index)
}
