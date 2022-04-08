package controlapi

import (
	"context"
	"fmt"
	"testing"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/testutils"
)

const testExtName = "testExtension"

func prepResource(t *testing.T, ts *testServer) *api.Resource {
	extensionReq := &api.CreateExtensionRequest{
		Annotations: &api.Annotations{
			Name: testExtName,
		},
	}
	// create an extension so we can create valid requests
	extResp, err := ts.Client.CreateExtension(context.Background(), extensionReq)
	assert.NoError(t, err)
	assert.NotNil(t, extResp)
	assert.NotNil(t, extResp.Extension)

	// We need to stuff some arbitrary data into an Any for this, so let's
	// create a simple Annnotations object
	anyContent := &api.Annotations{
		Name:   "SomeName",
		Labels: map[string]string{"some": "label"},
	}
	anyMsg, err := gogotypes.MarshalAny(anyContent)
	require.NoError(t, err)

	// first, create a valid resource, to ensure that that works
	req := &api.CreateResourceRequest{
		Annotations: &api.Annotations{
			Name: "ValidResource",
		},
		Kind:    testExtName,
		Payload: anyMsg,
	}

	createResp, err := ts.Client.CreateResource(context.Background(), req)
	require.NoError(t, err)
	require.NotNil(t, createResp)
	require.NotNil(t, createResp.Resource)
	return createResp.Resource
}

func TestCreateResource(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	extensionReq := &api.CreateExtensionRequest{
		Annotations: &api.Annotations{
			Name: testExtName,
		},
	}

	// create an extension so we can create valid requests
	resp, err := ts.Client.CreateExtension(context.Background(), extensionReq)
	assert.NoError(t, err)
	assert.NotNil(t, resp)
	assert.NotNil(t, resp.Extension)

	// first, create a valid resource, to ensure that that works
	t.Run("ValidResource", func(t *testing.T) {
		req := &api.CreateResourceRequest{
			Annotations: &api.Annotations{
				Name: "ValidResource",
			},
			Kind:    testExtName,
			Payload: &gogotypes.Any{},
		}

		resp, err := ts.Client.CreateResource(context.Background(), req)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
		assert.NotNil(t, resp.Resource)
		assert.NotEmpty(t, resp.Resource.ID)
		assert.NotZero(t, resp.Resource.Meta.Version.Index)
	})

	// then, test all the error cases.
	for _, tc := range []struct {
		name string
		req  *api.CreateResourceRequest
		code codes.Code
	}{
		{
			name: "NilAnnotations",
			req:  &api.CreateResourceRequest{},
			code: codes.InvalidArgument,
		}, {
			name: "EmptyName",
			req: &api.CreateResourceRequest{
				Annotations: &api.Annotations{
					// my gut says include a label in this test
					Labels: map[string]string{"name": "nope"},
				},
				Kind: testExtName,
			},
			code: codes.InvalidArgument,
		}, {
			name: "EmptyKind",
			req: &api.CreateResourceRequest{
				Annotations: &api.Annotations{
					Name: "EmptyKind",
				},
			},
			code: codes.InvalidArgument,
		}, {
			name: "InvalidKind",
			req: &api.CreateResourceRequest{
				Annotations: &api.Annotations{
					Name: "InvalidKind",
				},
				Kind: "notARealKind",
			},
			code: codes.InvalidArgument,
		}, {
			name: "NameConflict",
			req: &api.CreateResourceRequest{
				Annotations: &api.Annotations{
					Name: "ValidResource",
				},
				Kind:    testExtName,
				Payload: &gogotypes.Any{},
			},
			code: codes.AlreadyExists,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ts.Client.CreateResource(context.Background(), tc.req)
			assert.Error(t, err)
			assert.Equal(t, tc.code, testutils.ErrorCode(err), testutils.ErrorDesc(err))
		})
	}
}

func TestUpdateResourceValid(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	resource := prepResource(t, ts)
	resourceID := resource.ID

	for _, tc := range []struct {
		name string
		// transform mutates the resource to the expected form, and generates a
		// request for it
		transform func(*testing.T, *api.Resource) *api.UpdateResourceRequest
	}{
		{
			name: "Both",
			transform: func(t *testing.T, r *api.Resource) *api.UpdateResourceRequest {
				newAnnotations := &api.Annotations{
					Name:   "ValidResource",
					Labels: map[string]string{"some": "newlabels"},
				}
				newContent := &api.Annotations{
					Name: "SomeNewName",
				}
				newMsg, err := gogotypes.MarshalAny(newContent)
				require.NoError(t, err)
				r.Annotations = *newAnnotations
				r.Payload = newMsg

				return &api.UpdateResourceRequest{
					ResourceID:      resourceID,
					ResourceVersion: &r.Meta.Version,
					Annotations:     newAnnotations,
					Payload:         newMsg,
				}
			},
		}, {
			name: "OnlyAnnotations",
			transform: func(t *testing.T, r *api.Resource) *api.UpdateResourceRequest {
				r.Annotations.Labels = map[string]string{"onlyUpdating": "theseLabels"}
				return &api.UpdateResourceRequest{
					ResourceID:      r.ID,
					ResourceVersion: &r.Meta.Version,
					Annotations:     &r.Annotations,
				}
			},
		}, {
			name: "OnlyPayload",
			transform: func(t *testing.T, r *api.Resource) *api.UpdateResourceRequest {
				newContent := &api.Annotations{
					Name: "OnlyUpdatingPayload",
				}
				newMsg, err := gogotypes.MarshalAny(newContent)
				require.NoError(t, err)
				r.Payload = newMsg
				return &api.UpdateResourceRequest{
					ResourceID:      resourceID,
					ResourceVersion: &r.Meta.Version,
					Payload:         newMsg,
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var r *api.Resource
			ts.Store.View(func(tx store.ReadTx) {
				r = store.GetResource(tx, resourceID)
			})
			req := tc.transform(t, r)
			resp, err := ts.Client.UpdateResource(context.Background(), req)

			require.NoError(t, err)
			require.NotNil(t, resp)
			require.NotNil(t, resp.Resource)
			assert.Equal(t, resp.Resource.Payload, r.Payload)
			assert.Equal(t, resp.Resource.Annotations, r.Annotations)
			assert.Equal(t, resp.Resource.Kind, testExtName)
		})
	}
}

func TestUpdateResourceInvalid(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	resource := prepResource(t, ts)

	for _, tc := range []struct {
		name string
		req  *api.UpdateResourceRequest
		code codes.Code
	}{
		{
			name: "MissingID",
			req: &api.UpdateResourceRequest{
				ResourceID:      "",
				ResourceVersion: &resource.Meta.Version,
			},
			code: codes.InvalidArgument,
		}, {
			name: "MissingVersion",
			req: &api.UpdateResourceRequest{
				ResourceID: resource.ID,
			},
			code: codes.InvalidArgument,
		}, {
			name: "NotFound",
			req: &api.UpdateResourceRequest{
				ResourceID:      "notreal",
				ResourceVersion: &resource.Meta.Version,
			},
			code: codes.NotFound,
		}, {
			name: "IncorrectVersion",
			req: &api.UpdateResourceRequest{
				ResourceID:      resource.ID,
				ResourceVersion: &api.Version{Index: 0},
			},
			code: codes.InvalidArgument,
		}, {
			name: "ChangedName",
			req: &api.UpdateResourceRequest{
				ResourceID:      resource.ID,
				ResourceVersion: &resource.Meta.Version,
				Annotations: &api.Annotations{
					Name: "different",
				},
			},
			code: codes.InvalidArgument,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ts.Client.UpdateResource(context.Background(), tc.req)
			assert.Error(t, err)
			assert.Equal(t, tc.code, testutils.ErrorCode(err), testutils.ErrorDesc(err))
		})
	}
}

func TestGetResource(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	resource := prepResource(t, ts)

	// empty resource ID should fail with invalid argument
	resp, err := ts.Client.GetResource(context.Background(), &api.GetResourceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// id which does not exist should return NotFound
	resp, err = ts.Client.GetResource(
		context.Background(),
		&api.GetResourceRequest{ResourceID: "notreal"},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))

	// ID which exists should return the resource
	resp, err = ts.Client.GetResource(
		context.Background(),
		&api.GetResourceRequest{ResourceID: resource.ID},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Resource)
	assert.Equal(t, resp.Resource, resource)
}

func TestRemoveResource(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	resource := prepResource(t, ts)

	// empty resource ID should fail with invalid argument
	resp, err := ts.Client.RemoveResource(context.Background(), &api.RemoveResourceRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err), testutils.ErrorDesc(err))
	assert.Nil(t, resp)

	// id which does not exist should return NotFound
	resp, err = ts.Client.RemoveResource(
		context.Background(),
		&api.RemoveResourceRequest{ResourceID: "notreal"},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))
	assert.Nil(t, resp)

	// ID which exists should return the resource
	resp, err = ts.Client.RemoveResource(
		context.Background(),
		&api.RemoveResourceRequest{ResourceID: resource.ID},
	)
	require.NoError(t, err)
	require.NotNil(t, resp)

	// Trying to delete again should return not found
	resp, err = ts.Client.RemoveResource(
		context.Background(),
		&api.RemoveResourceRequest{ResourceID: resource.ID},
	)
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err), testutils.ErrorDesc(err))
	assert.Nil(t, resp)
}

func TestListResources(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	kinds := []string{"kind1", "kind2", "kind3"}
	for _, kind := range kinds {
		// create some extensions
		_, err := ts.Client.CreateExtension(
			context.Background(), &api.CreateExtensionRequest{
				Annotations: &api.Annotations{Name: kind},
			},
		)
		require.NoError(t, err)
	}

	// everything beyond this point is pretty much copied verbatim from
	// config_test.go, but changed for resources and to also test kinds

	listResources := func(req *api.ListResourcesRequest) map[string]*api.Resource {
		resp, err := ts.Client.ListResources(context.Background(), req)
		require.NoError(t, err)
		require.NotNil(t, resp)

		byName := make(map[string]*api.Resource)
		for _, resource := range resp.Resources {
			byName[resource.Annotations.Name] = resource
		}
		return byName
	}

	// listing when there is nothing returns an empty list but no error
	result := listResources(&api.ListResourcesRequest{})
	assert.Len(t, result, 0)

	// create a bunch of resources to test filtering
	allListableNames := []string{"aaa", "aab", "abc", "bbb", "bac", "bbc", "ccc", "cac", "cbc", "ddd"}
	resourceNamesToID := make(map[string]string)
	for i, resourceName := range allListableNames {
		resp, err := ts.Client.CreateResource(
			context.Background(), &api.CreateResourceRequest{
				Annotations: &api.Annotations{
					Name: resourceName,
					Labels: map[string]string{
						"mod2": fmt.Sprintf("%d", i%2),
						"mod4": fmt.Sprintf("%d", i%4),
					},
				},
				Kind: kinds[i%len(kinds)],
			},
		)
		require.NoError(t, err)
		resourceNamesToID[resourceName] = resp.Resource.ID
	}

	type listTestCase struct {
		desc     string
		expected []string
		filter   *api.ListResourcesRequest_Filters
	}

	listResourceTestCases := []listTestCase{
		{
			desc:     "no filter: all available resources are returned",
			expected: allListableNames,
			filter:   nil,
		}, {
			desc:     "searching for something that doesn't match returns an empty list",
			expected: nil,
			filter:   &api.ListResourcesRequest_Filters{Names: []string{"aa"}},
		}, {
			desc:     "multiple name filters are or-ed together",
			expected: []string{"aaa", "bbb", "ccc"},
			filter:   &api.ListResourcesRequest_Filters{Names: []string{"aaa", "bbb", "ccc"}},
		}, {
			desc:     "multiple name prefix filters are or-ed together",
			expected: []string{"aaa", "aab", "bbb", "bbc"},
			filter:   &api.ListResourcesRequest_Filters{NamePrefixes: []string{"aa", "bb"}},
		}, {
			desc:     "multiple ID prefix filters are or-ed together",
			expected: []string{"aaa", "bbb"},
			filter: &api.ListResourcesRequest_Filters{IDPrefixes: []string{
				resourceNamesToID["aaa"], resourceNamesToID["bbb"]},
			},
		}, {
			desc:     "name prefix, name, and ID prefix filters are or-ed together",
			expected: []string{"aaa", "aab", "bbb", "bbc", "ccc", "ddd"},
			filter: &api.ListResourcesRequest_Filters{
				Names:        []string{"aaa", "ccc"},
				NamePrefixes: []string{"aa", "bb"},
				IDPrefixes:   []string{resourceNamesToID["aaa"], resourceNamesToID["ddd"]},
			},
		}, {
			desc:     "all labels in the label map must be matched",
			expected: []string{allListableNames[0], allListableNames[4], allListableNames[8]},
			filter: &api.ListResourcesRequest_Filters{
				Labels: map[string]string{
					"mod2": "0",
					"mod4": "0",
				},
			},
		}, {
			desc: "name prefix, name, and ID prefix filters are or-ed together, but the results must match all labels in the label map",
			// + indicates that these would be selected with the name/id/prefix filtering, and 0/1 at the end indicate the mod2 value:
			// +"aaa"0, +"aab"1, "abc"0, +"bbb"1, "bac"0, +"bbc"1, +"ccc"0, "cac"1, "cbc"0, +"ddd"1
			expected: []string{"aaa", "ccc"},
			filter: &api.ListResourcesRequest_Filters{
				Names:        []string{"aaa", "ccc"},
				NamePrefixes: []string{"aa", "bb"},
				IDPrefixes:   []string{resourceNamesToID["aaa"], resourceNamesToID["ddd"]},
				Labels: map[string]string{
					"mod2": "0",
				},
			},
		}, {
			desc: "extension filter matches only specified extension",
			expected: []string{
				allListableNames[0], allListableNames[3],
				allListableNames[6], allListableNames[9],
			},
			filter: &api.ListResourcesRequest_Filters{
				Kind: kinds[0],
			},
		}, {
			desc:     "name prefix, name, and ID prefix filters are or-ed together, but the results must match all labels in the label map and the extension",
			expected: []string{"aaa"},
			// without label and extension filters, we would have:
			// expected: []string{"aaa", "aab", "abc", "bbb", "bbc", "ddd"},
			// when we mod2, we get left with aaa and abc
			// when we filter on kinds[0], we're left with just aaa (as abc is
			// kinds[0])
			filter: &api.ListResourcesRequest_Filters{
				Names:        []string{"aaa", "abc"},
				NamePrefixes: []string{"aa", "bb"},
				IDPrefixes:   []string{resourceNamesToID["aaa"], resourceNamesToID["ddd"]},
				Labels: map[string]string{
					"mod2": "0",
				},
				Kind: kinds[0],
			},
		},
	}

	for _, expectation := range listResourceTestCases {
		result := listResources(&api.ListResourcesRequest{Filters: expectation.filter})
		assert.Len(t, result, len(expectation.expected), expectation.desc)
		for _, name := range expectation.expected {
			assert.Contains(t, result, name, expectation.desc)
			assert.NotNil(t, result[name], expectation.desc)
			assert.Equal(t, resourceNamesToID[name], result[name].ID, expectation.desc)
		}
	}
}
