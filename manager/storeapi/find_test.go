package storeapi

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func TestFindObjects(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	createNode(t, ts, "id1", api.NodeRoleManager, api.NodeMembershipAccepted, api.NodeStatus_READY)
	createNode(t, ts, "id2", api.NodeRoleWorker, api.NodeMembershipAccepted, api.NodeStatus_READY)
	createNode(t, ts, "id3", api.NodeRoleWorker, api.NodeMembershipPending, api.NodeStatus_READY)
	createNode(t, ts, "id11", api.NodeRoleWorker, api.NodeMembershipPending, api.NodeStatus_READY)

	// Kind is required
	_, err := ts.Client.FindObjects(ctx, &api.FindObjectsRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	// Should return all nodes
	results, err := ts.Client.FindObjects(ctx, &api.FindObjectsRequest{Kind: "node"})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 4)

	// Should return no nodes
	results, err = ts.Client.FindObjects(ctx, &api.FindObjectsRequest{
		Kind: "node",
		Selectors: []*api.SelectBy{
			{
				By: &api.SelectBy_Name{Name: "doesnotexist"},
			},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 0)

	// Should return the worker nodes
	results, err = ts.Client.FindObjects(ctx, &api.FindObjectsRequest{
		Kind: "node",
		Selectors: []*api.SelectBy{
			{
				By: &api.SelectBy_Role{Role: api.NodeRoleWorker},
			},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 3)

	// Should return nodes id1 and id11
	results, err = ts.Client.FindObjects(ctx, &api.FindObjectsRequest{
		Kind: "node",
		Selectors: []*api.SelectBy{
			{
				By: &api.SelectBy_IDPrefix{IDPrefix: "id1"},
			},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 2)

	// Should return nodes id1, id3, and id11
	results, err = ts.Client.FindObjects(ctx, &api.FindObjectsRequest{
		Kind: "node",
		Selectors: []*api.SelectBy{
			{
				By: &api.SelectBy_Role{Role: api.NodeRoleManager},
			},
			{
				By: &api.SelectBy_Membership{Membership: api.NodeMembershipPending},
			},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 3)

	// Overly broad criteria that matches nodes more than once
	results, err = ts.Client.FindObjects(ctx, &api.FindObjectsRequest{
		Kind: "node",
		Selectors: []*api.SelectBy{
			{
				By: &api.SelectBy_Role{Role: api.NodeRoleManager},
			},
			{
				By: &api.SelectBy_Role{Role: api.NodeRoleWorker},
			},
			{
				By: &api.SelectBy_Membership{Membership: api.NodeMembershipPending},
			},
			{
				By: &api.SelectBy_Membership{Membership: api.NodeMembershipAccepted},
			},
			{
				By: &api.SelectBy_IDPrefix{IDPrefix: "id1"},
			},
		},
	})
	assert.NoError(t, err)
	assert.Len(t, results.Results, 4)

}
