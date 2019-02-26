package controlapi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/testutils"
)

func TestCreateExtension(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// ---- CreateExtensionRequest with no Annotations fails ----
	_, err := ts.Client.CreateExtension(context.Background(), &api.CreateExtensionRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// --- With no name also fails
	_, err = ts.Client.CreateExtension(context.Background(),
		&api.CreateExtensionRequest{
			Annotations: &api.Annotations{
				Name:   "",
				Labels: map[string]string{"foo": "bar"},
			},
		},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	extensionName := "extension1"
	// ---- creating an extension with a valid extension object passed in succeeds ----
	validRequest := api.CreateExtensionRequest{Annotations: &api.Annotations{Name: extensionName}}

	resp, err := ts.Client.CreateExtension(context.Background(), &validRequest)
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	// for sanity, check that the stored extension still has the extension data
	var storedExtension *api.Extension
	ts.Store.View(func(tx store.ReadTx) {
		storedExtension = store.GetExtension(tx, resp.Extension.ID)
	})
	assert.NotNil(t, storedExtension)
	assert.Equal(t, extensionName, storedExtension.Annotations.Name)

	// ---- creating an extension with the same name, even if it's the exact same spec, fails due to a name conflict ----
	_, err = ts.Client.CreateExtension(context.Background(), &validRequest)
	assert.Error(t, err)
	assert.Equal(t, codes.AlreadyExists, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// creating an extension with an empty string as a name fails
	hasNoName := api.CreateExtensionRequest{
		Annotations: &api.Annotations{
			Labels: map[string]string{"name": "nope"},
		},
		Description: "some text",
	}
	_, err = ts.Client.CreateExtension(
		context.Background(), &hasNoName,
	)
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))
}

func TestGetExtension(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// ---- getting an extension without providing an ID results in an InvalidArgument ----
	_, err := ts.Client.GetExtension(context.Background(), &api.GetExtensionRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// ---- getting a non-existent extension fails with NotFound ----
	_, err = ts.Client.GetExtension(context.Background(), &api.GetExtensionRequest{ExtensionID: "12345"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// ---- getting an existing extension returns the extension ----
	extensionName := "extension1"
	validRequest := api.CreateExtensionRequest{Annotations: &api.Annotations{Name: extensionName}}
	resp, err := ts.Client.CreateExtension(context.Background(), &validRequest)
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	resp1, err := ts.Client.GetExtension(context.Background(), &api.GetExtensionRequest{ExtensionID: resp.Extension.ID})
	assert.NoError(t, err)
	assert.NotNil(t, resp1)
	assert.NotNil(t, resp1)
	assert.Equal(t, validRequest.Annotations.Name, resp1.Extension.Annotations.Name)
}

// Test removing an extension that has no resources of that kind present.
func TestRemoveUnreferencedExtension(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// removing an extension without providing an ID results in an InvalidArgument
	_, err := ts.Client.RemoveExtension(context.Background(), &api.RemoveExtensionRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// removing an extension that exists succeeds
	extensionName := "extension1"
	validRequest := api.CreateExtensionRequest{Annotations: &api.Annotations{Name: extensionName}}
	resp, err := ts.Client.CreateExtension(context.Background(), &validRequest)
	assert.NoError(t, err)
	assert.NotNil(t, resp)

	resp1, err := ts.Client.RemoveExtension(context.Background(), &api.RemoveExtensionRequest{ExtensionID: resp.Extension.ID})
	assert.NoError(t, err)
	assert.Equal(t, api.RemoveExtensionResponse{}, *resp1)

	// ---- verify the extension was really removed because attempting to remove it again fails with a NotFound ----
	_, err = ts.Client.RemoveExtension(context.Background(), &api.RemoveExtensionRequest{ExtensionID: resp.Extension.ID})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

}

// Test removing an extension that has resources of that kind present.
func TestRemoveReferencedExtension(t *testing.T) {
	// TDB after resource APIs are implemented
}
