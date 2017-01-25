package storeapi

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestUpdateObjects(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	nodeObj := &api.Object{Object: &api.Object_Node{Node: &api.Node{
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipAccepted,
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRoleManager,
	}}}

	extensionObj := &api.Object{Object: &api.Object_Extension{Extension: &api.Extension{
		Annotations: api.Annotations{
			Name: "widget",
		},
	}}}

	resourceObj := &api.Object{Object: &api.Object_Resource{Resource: &api.Resource{
		Kind: "widget",
		Payload: &gogotypes.Any{
			TypeUrl: "http://domain.ext/typespec",
			Value:   []byte("abcdef"),
		},
	}}}

	returnedNode, err := ts.Client.CreateObject(ctx, nodeObj)
	assert.NoError(t, err)
	_, err = ts.Client.CreateObject(ctx, extensionObj)
	assert.NoError(t, err)
	returnedResource, err := ts.Client.CreateObject(ctx, resourceObj)
	assert.NoError(t, err)

	// Update one object.
	updatedNode := returnedNode.GetNode().Copy()
	updatedNode.Role = api.NodeRoleWorker
	returnedObjects, err := ts.Client.UpdateObjects(ctx, &api.UpdateObjectsRequest{
		Objects: []*api.Object{{Object: &api.Object_Node{Node: updatedNode}}},
	})
	assert.NoError(t, err)
	require.Len(t, returnedObjects.Objects, 1)
	var storeNode *api.Node
	ts.Store.View(func(readTx store.ReadTx) {
		storeNode = store.GetNode(readTx, returnedNode.GetNode().ID)
	})
	assert.Equal(t, api.NodeRoleWorker, storeNode.Role)
	assert.Equal(t, storeNode, returnedObjects.Objects[0].GetNode())

	// Update two objects at once.
	updatedNode = storeNode.Copy()
	updatedNode.Status.State = api.NodeStatus_DOWN
	updatedResource := returnedResource.GetResource().Copy()
	updatedResource.Payload.Value = []byte("defg")
	returnedObjects, err = ts.Client.UpdateObjects(ctx, &api.UpdateObjectsRequest{
		Objects: []*api.Object{
			{Object: &api.Object_Node{Node: updatedNode}},
			{Object: &api.Object_Resource{Resource: updatedResource}},
		},
	})
	assert.NoError(t, err)
	require.Len(t, returnedObjects.Objects, 2)
	var storeResource *api.Resource
	ts.Store.View(func(readTx store.ReadTx) {
		storeNode = store.GetNode(readTx, returnedNode.GetNode().ID)
		storeResource = store.GetResource(readTx, returnedResource.GetResource().ID)
	})
	assert.Equal(t, api.NodeStatus_DOWN, storeNode.Status.State)
	assert.Equal(t, storeNode, returnedObjects.Objects[0].GetNode())
	assert.Equal(t, []byte("defg"), storeResource.Payload.Value)
	assert.Equal(t, storeResource, returnedObjects.Objects[1].GetResource())

	// Try to update two objects, where one is out of date. The whole
	// update should fail and no data should change.
	updatedNode = storeNode.Copy()
	updatedNode.Status.State = api.NodeStatus_READY
	// note: updatedResource NOT up to date here
	updatedResource.Payload.Value = []byte("xyz")
	returnedObjects, err = ts.Client.UpdateObjects(ctx, &api.UpdateObjectsRequest{
		Objects: []*api.Object{
			{Object: &api.Object_Node{Node: updatedNode}},
			{Object: &api.Object_Resource{Resource: updatedResource}},
		},
	})
	assert.Error(t, err)
	assert.Nil(t, returnedObjects)
	ts.Store.View(func(readTx store.ReadTx) {
		storeNode = store.GetNode(readTx, returnedNode.GetNode().ID)
		storeResource = store.GetResource(readTx, returnedResource.GetResource().ID)
	})
	assert.Equal(t, api.NodeStatus_DOWN, storeNode.Status.State)
	assert.Equal(t, []byte("defg"), storeResource.Payload.Value)
}
