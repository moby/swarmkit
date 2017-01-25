package storeapi

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestWatch(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	// Watch for node creates
	watch, err := ts.Client.Watch(ctx, &api.WatchRequest{
		Entries: []*api.WatchRequest_WatchEntry{
			{
				Kind:   "node",
				Action: api.StoreActionKindCreate,
			},
		},
	})
	assert.NoError(t, err)

	// Should receive an initial message that indicates the watch is ready
	msg, err := watch.Recv()
	assert.NoError(t, err)
	assert.Equal(t, &api.WatchMessage{}, msg)

	createNode(t, ts, "id1", api.NodeRoleManager, api.NodeMembershipAccepted, api.NodeStatus_READY)
	msg, err = watch.Recv()
	assert.NoError(t, err)
	assert.Equal(t, api.StoreActionKindCreate, msg.Action)
	require.NotNil(t, msg.Object.GetNode())
	assert.Equal(t, "id1", msg.Object.GetNode().ID)

	watch.CloseSend()

	// Watch for node creates that match a name prefix and a custom index, or
	// are managers
	watch, err = ts.Client.Watch(ctx, &api.WatchRequest{
		Entries: []*api.WatchRequest_WatchEntry{
			{
				Kind:   "node",
				Action: api.StoreActionKindCreate,
				Filters: []*api.SelectBy{
					{
						By: &api.SelectBy_NamePrefix{
							NamePrefix: "east",
						},
					},
					{
						By: &api.SelectBy_Custom{
							Custom: &api.SelectByCustom{
								Index: "myindex",
								Value: "myval",
							},
						},
					},
				},
			},
			{
				Kind:   "node",
				Action: api.StoreActionKindCreate,
				Filters: []*api.SelectBy{
					{
						By: &api.SelectBy_Role{
							Role: api.NodeRoleManager,
						},
					},
				},
			},
		},
	})
	assert.NoError(t, err)

	// Should receive an initial message that indicates the watch is ready
	msg, err = watch.Recv()
	assert.NoError(t, err)
	assert.Equal(t, &api.WatchMessage{}, msg)

	createNode(t, ts, "id2", api.NodeRoleManager, api.NodeMembershipAccepted, api.NodeStatus_READY)
	msg, err = watch.Recv()
	assert.NoError(t, err)
	assert.Equal(t, api.StoreActionKindCreate, msg.Action)
	require.NotNil(t, msg.Object.GetNode())
	assert.Equal(t, "id2", msg.Object.GetNode().ID)

	// Shouldn't be seen by the watch
	createNode(t, ts, "id3", api.NodeRoleWorker, api.NodeMembershipAccepted, api.NodeStatus_READY)

	// Shouldn't be seen either - no hostname
	node := &api.Node{
		ID: "id4",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Indices: []api.IndexEntry{
					{Key: "myindex", Val: "myval"},
				},
			},
		},
		Role: api.NodeRoleWorker,
	}
	err = ts.Store.Update(func(tx store.Tx) error {
		return store.CreateNode(tx, node)
	})
	assert.NoError(t, err)

	// Shouldn't be seen either - hostname doesn't match filter
	node = &api.Node{
		ID: "id5",
		Description: &api.NodeDescription{
			Hostname: "west-40",
		},
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Indices: []api.IndexEntry{
					{Key: "myindex", Val: "myval"},
				},
			},
		},
		Role: api.NodeRoleWorker,
	}
	err = ts.Store.Update(func(tx store.Tx) error {
		return store.CreateNode(tx, node)
	})
	assert.NoError(t, err)

	// This one should be seen
	node = &api.Node{
		ID: "id6",
		Description: &api.NodeDescription{
			Hostname: "east-95",
		},
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Indices: []api.IndexEntry{
					{Key: "myindex", Val: "myval"},
				},
			},
		},
		Role: api.NodeRoleWorker,
	}
	err = ts.Store.Update(func(tx store.Tx) error {
		return store.CreateNode(tx, node)
	})
	assert.NoError(t, err)

	msg, err = watch.Recv()
	assert.NoError(t, err)
	assert.Equal(t, api.StoreActionKindCreate, msg.Action)
	require.NotNil(t, msg.Object.GetNode())
	assert.Equal(t, "id6", msg.Object.GetNode().ID)

	watch.CloseSend()
}
