package controlapi

import (
	// testing imports
	"github.com/docker/swarmkit/testutils"

	// using both assert and require. assert is used for things that are under
	// test, and require is used for things that aren't under test, and which
	// if failed make the whole test useless
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"

	"context"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"
	"google.golang.org/grpc/codes"
)

// tests of the DeviceClass API. the tests in this file have been largely
// modeled off of the tests of the other API methods.

func TestCreateDeviceClass(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// aliasing context.Background as ctx so i don't have to keep typing it
	ctx := context.Background()

	// creating a DeviceClass with an invalid spec fails, verifying that
	// CreateDeviceClass validates the spec
	_, err := ts.Client.CreateDeviceClass(ctx, &api.CreateDeviceClassRequest{
		// all we really have to do here is set an invalid annotations,
		// such as one with no name. we can do this by just creating an
		// empty spec
		Spec: &api.DeviceClassSpec{},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// creating a DeviceClass with a valid spec should succeed, and return a
	// DeviceClass that's the same as in the store.
	deviceClassSpec := &api.DeviceClassSpec{
		Annotations: api.Annotations{
			Name: "fooclass",
			// Including labels, just to be certain that they work.
			Labels: map[string]string{
				"foolabel": "foovalue", "barlabel": "barvalue",
			},
		},
		// Shared is the only other field in the spec.
		Shared: true,
	}
	validReq := &api.CreateDeviceClassRequest{Spec: deviceClassSpec}

	resp, err := ts.Client.CreateDeviceClass(context.Background(), validReq)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.DeviceClass)
	assert.Equal(t, *deviceClassSpec, resp.DeviceClass.Spec)

	// now check that the stored DeviceClass is the same
	var storedDeviceClass *api.DeviceClass
	ts.Store.View(func(tx store.ReadTx) {
		storedDeviceClass = store.GetDeviceClass(tx, resp.DeviceClass.ID)
	})
	assert.NotNil(t, storedDeviceClass)
	assert.Equal(t, resp.DeviceClass.Spec, storedDeviceClass.Spec)

	// creating a DeviceClass with the same name should fail with a name
	// conflict
	_, err = ts.Client.CreateDeviceClass(ctx, validReq)
	assert.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, testutils.ErrorCode(err), testutils.ErrorDesc(err))
}

func TestGetDeviceClass(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// alias context.Background() for ergonomics
	ctx := context.Background()

	// with an ID, return InvalidArgument
	_, err := ts.Client.GetDeviceClass(ctx, &api.GetDeviceClassRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// if the device class does not exist, return NotFound
	_, err = ts.Client.GetDeviceClass(ctx, &api.GetDeviceClassRequest{DeviceClassID: "notreal"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// getting an existing device class correctly returns the device class
	deviceClass := &api.DeviceClass{
		ID: identity.NewID(),
		Spec: api.DeviceClassSpec{
			Annotations: api.Annotations{
				Name: "fooclass",
			},
		},
	}
	err = ts.Store.Update(func(tx store.Tx) error {
		return store.CreateDeviceClass(tx, deviceClass)
	})
	require.NoError(t, err)

	resp, err := ts.Client.GetDeviceClass(ctx, &api.GetDeviceClassRequest{
		DeviceClassID: deviceClass.ID,
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.DeviceClass)
	assert.Equal(t, deviceClass, resp.DeviceClass)
}

func TestUpdateDeviceClass(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// again, aliasing context.Background()
	ctx := context.Background()

	// add a device class to the store so we have something to update
	deviceClass := &api.DeviceClass{
		ID: identity.NewID(),
		Spec: api.DeviceClassSpec{
			Annotations: api.Annotations{
				Name:   "fooclass",
				Labels: map[string]string{"foolabel": "foovalue"},
			},
		},
	}

	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateDeviceClass(tx, deviceClass)
	})
	require.NoError(t, err)

	// updating without a DeviceClass gives InvalidArgument
	_, err = ts.Client.UpdateDeviceClass(ctx, &api.UpdateDeviceClassRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// trying with a nonexistent DeviceClass results in NotFound
	_, err = ts.Client.UpdateDeviceClass(ctx, &api.UpdateDeviceClassRequest{
		DeviceClassID:      "notreal",
		DeviceClassVersion: &api.Version{Index: 1},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// updating name results in an error
	deviceClass.Spec.Annotations.Name = "newname"
	resp, err := ts.Client.UpdateDeviceClass(ctx, &api.UpdateDeviceClassRequest{
		DeviceClassID:      deviceClass.ID,
		Spec:               &deviceClass.Spec,
		DeviceClassVersion: &deviceClass.Meta.Version,
	})
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// updating the DeviceClass with the original spec succeeds
	deviceClass.Spec.Annotations.Name = "fooclass"
	resp, err = ts.Client.UpdateDeviceClass(ctx, &api.UpdateDeviceClassRequest{
		DeviceClassID:      deviceClass.ID,
		Spec:               &deviceClass.Spec,
		DeviceClassVersion: &deviceClass.Meta.Version,
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.DeviceClass)

	// updating the labels succeeds
	deviceClass.Spec.Annotations.Labels = map[string]string{
		"foolabel": "different", "barlabel": "new",
	}
	resp, err = ts.Client.UpdateDeviceClass(ctx, &api.UpdateDeviceClassRequest{
		DeviceClassID: deviceClass.ID,
		Spec:          &deviceClass.Spec,
		// need to use the updated Version
		DeviceClassVersion: &resp.DeviceClass.Meta.Version,
	})
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.DeviceClass)
	assert.Equal(t, deviceClass.Spec.Annotations.Labels, resp.DeviceClass.Spec.Annotations.Labels)
}

func TestRemoveDeviceClassUnused(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// alias context.Background() for ergonomics
	ctx := context.Background()

	// removing a device class without providing an ID results in an
	// InvalidArgument
	_, err := ts.Client.RemoveDeviceClass(ctx, &api.RemoveDeviceClassRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// removing a device class that exists succeeds
	deviceClass := &api.DeviceClass{
		ID: identity.NewID(),
		Spec: api.DeviceClassSpec{
			Annotations: api.Annotations{
				Name: "fooname",
			},
		},
	}
	err = ts.Store.Update(func(tx store.Tx) error {
		return store.CreateDeviceClass(tx, deviceClass)
	})
	require.NoError(t, err)

	resp, err := ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass.ID},
	)
	assert.NoError(t, err)
	assert.Equal(t, api.RemoveDeviceClassResponse{}, *resp)

	// verify that it's actually gone by trying to remove it a second time
	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass.ID},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))
}

func TestRemoveDeviceClassUsed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	ctx := context.Background()

	// create two device classes
	deviceClass1 := &api.DeviceClass{
		ID: identity.NewID(),
		Spec: api.DeviceClassSpec{
			Annotations: api.Annotations{
				Name: "deviceClass1",
			},
		},
	}

	deviceClass2 := &api.DeviceClass{
		ID: identity.NewID(),
		Spec: api.DeviceClassSpec{
			Annotations: api.Annotations{
				Name: "deviceClass2",
			},
		},
	}

	// now add a node with a device in one class
	node := &api.Node{
		ID: identity.NewID(),
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipAccepted,
			Devices: []*api.Device{
				&api.Device{
					DeviceClassID: deviceClass1.ID,
					Path:          "/dev/foo",
				},
			},
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRoleManager,
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateDeviceClass(tx, deviceClass1))
		assert.NoError(t, store.CreateDeviceClass(tx, deviceClass2))
		assert.NoError(t, store.CreateNode(tx, node))
		return nil
	})
	require.NoError(t, err)

	// removing a DeviceClass that exists but is in use should fail
	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass1.ID},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// removing a DeviceClass that exists but is not in use should succeed
	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass2.ID},
	)
	assert.NoError(t, err)

	// update the node to no longer have that Device
	node.Spec.Devices = []*api.Device{}
	// throw away the response -- we don't care about it in this test
	_, err = ts.Client.UpdateNode(ctx, &api.UpdateNodeRequest{
		NodeID:      node.ID,
		NodeVersion: &node.Meta.Version,
		Spec:        &node.Spec,
	})
	require.NoError(t, err)

	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass1.ID},
	)
	assert.NoError(t, err)

	// make sure both of these are really removed
	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass1.ID},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	_, err = ts.Client.RemoveDeviceClass(
		ctx, &api.RemoveDeviceClassRequest{DeviceClassID: deviceClass2.ID},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))
}

func TestListDeviceClasses(t *testing.T) {
	t.Skip("(dperny) this mostly tests, like, filters, so we'll punt on it for now")
}
