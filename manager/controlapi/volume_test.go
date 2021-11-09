package controlapi

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/testutils"
)

const testVolumeDriver = "somedriver"

// cannedVolume provides a volume object for testing volume operations. always
// copy before using, to avoid affecting other tests.
var cannedVolume = &api.Volume{
	ID: "someid",
	Spec: api.VolumeSpec{
		Annotations: api.Annotations{
			Name: "somename",
		},
		Group: "somegroup",
		Driver: &api.Driver{
			Name: testVolumeDriver,
		},
		// use defaults for access mode.
		AccessMode: &api.VolumeAccessMode{
			// use block access mode because it has no fields
			AccessType: &api.VolumeAccessMode_Block{
				Block: &api.VolumeAccessMode_BlockVolume{},
			},
		},
	},
}

// TestCreateVolumeEmptyRequest tests that calling CreateVolume with an empty
// request returns an error
func TestCreateVolumeEmptyRequest(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
}

// TestCreateVolumeNoName tests that creating a volume without setting a name
// returns an error
func TestCreateVolumeNoName(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	v := cannedVolume.Copy()
	v.Spec.Annotations = api.Annotations{}
	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &v.Spec,
	})

	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	assert.Contains(t, err.Error(), "name")
}

func TestCreateVolumeNoDriver(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	v := cannedVolume.Copy()
	v.Spec.Driver = nil

	// no driver
	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &v.Spec,
	})

	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	assert.Contains(t, err.Error(), "driver")
}

// TestCreateVolumeValid tests that creating a volume with valid parameters
// succeeds
func TestCreateVolumeValid(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	v := cannedVolume.Copy()

	resp, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &v.Spec,
	})

	assert.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Volume, "volume in response should not be nil")
	assert.NotEmpty(t, resp.Volume.ID, "volume ID should not be empty")
	assert.Equal(t, resp.Volume.Spec, v.Spec, "response spec should match request spec")

	var volume *api.Volume
	ts.Store.View(func(tx store.ReadTx) {
		volume = store.GetVolume(tx, resp.Volume.ID)
	})

	assert.NotNil(t, volume)
}

// TestCreateVolumeValidateSecrets tests that creating a volume that uses
// secrets validates that those secrets exist.
func TestCreateVolumeValidateSecrets(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	secrets := []*api.Secret{
		{
			ID: "someID1",
			Spec: api.SecretSpec{
				Annotations: api.Annotations{
					Name: "somename1",
				},
			},
		},
	}

	ts.Store.Update(func(tx store.Tx) error {
		for _, secret := range secrets {
			if err := store.CreateSecret(tx, secret); err != nil {
				return err
			}
		}
		return nil
	})

	v := cannedVolume.Copy()
	v.Spec.Secrets = []*api.VolumeSecret{
		{
			Key:    "foo",
			Secret: "someIDnotReal",
		}, {
			Key:    "bar",
			Secret: "someOtherNotRealID",
		},
	}

	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &v.Spec,
	})
	assert.Error(t, err, "expected creating a volume when a secret doesn't exist to fail")
	assert.Contains(t, err.Error(), "secret")
	assert.Contains(t, err.Error(), "someIDnotReal")
	assert.Contains(t, err.Error(), "someOtherNotRealID")

	// replace the secret with the ones that exist.
	v.Spec.Secrets = []*api.VolumeSecret{
		{
			Key:    "foo",
			Secret: "someID1",
		},
	}
	_, err = ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &v.Spec,
	})
	assert.NoError(t, err)
}

// TestCreateVolumeInvalidAccessMode tests that CreateVolume enforces the
// existence of the VolumeAccessMode, and the existence of the AccessType
// inside it.
func TestCreateVolumeInvalidAccessMode(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()
	volume.Spec.AccessMode = nil

	_, err := ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &volume.Spec,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "AccessMode must not be nil")

	volume.Spec.AccessMode = &api.VolumeAccessMode{}

	_, err = ts.Client.CreateVolume(context.Background(), &api.CreateVolumeRequest{
		Spec: &volume.Spec,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "either Mount or Block")
}

// TestUpdateVolume tests that correctly updating a volume succeeds.
func TestUpdateVolume(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()

	// create a volume. avoid using CreateVolume, because we want to test
	// UpdateVolume in isolation
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	})
	assert.NoError(t, err, "creating volume in store returned an error")

	// we need to read the volume back out so we can get the current version
	// just reuse the volume variable and store the latest copy in it.
	ts.Store.View(func(tx store.ReadTx) {
		volume = store.GetVolume(tx, volume.ID)
	})

	// for now, we can only update labels
	spec := volume.Spec.Copy()
	spec.Annotations.Labels = map[string]string{"foolabel": "waldo"}

	resp, err := ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		VolumeID:      volume.ID,
		VolumeVersion: &volume.Meta.Version,
		Spec:          spec,
	})

	assert.NoError(t, err, "expected updating volume to return no error")
	require.NotNil(t, resp, "response was nil")
	require.NotNil(t, resp.Volume, "response.Volume was nil")
	require.Equal(t, resp.Volume.ID, volume.ID)
	require.Equal(t, resp.Volume.Spec, *spec)

	// now get the updated volume from the store
	var updatedVolume *api.Volume
	ts.Store.View(func(tx store.ReadTx) {
		updatedVolume = store.GetVolume(tx, volume.ID)
	})
	require.NotNil(t, updatedVolume)
	assert.Equal(t, *spec, updatedVolume.Spec)
}

// TestUpdateVolumeMissingRequestComponents tests that an UpdateVolumeRequest
// missing any of its fields is invalid.
func TestUpdateVolumeMissingRequestComponents(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	// empty ID
	_, err := ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		Spec:          &cannedVolume.Spec,
		VolumeVersion: &api.Version{},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	assert.Contains(t, err.Error(), "ID")

	// empty spec
	_, err = ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		VolumeID:      cannedVolume.ID,
		VolumeVersion: &api.Version{},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	assert.Contains(t, err.Error(), "Spec")

	// empty version
	_, err = ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		VolumeID: cannedVolume.ID,
		Spec:     &cannedVolume.Spec,
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
	assert.Contains(t, err.Error(), "VolumeVersion")
}

// TestUpdateVolumeNotFound tests that trying to update a volume that does not
// exist returns a not found error
func TestUpdateVolumeNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		VolumeID:      cannedVolume.ID,
		Spec:          &cannedVolume.Spec,
		VolumeVersion: &api.Version{},
	})

	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))
}

// TestUpdateVolumeOutOfSequence tests that if the VolumeVersion is incorrect,
// an error is returned.
func TestUpdateVolumeOutOfSequence(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()

	// create a volume. avoid using CreateVolume, because we want to test
	// UpdateVolume in isolation
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	})
	assert.NoError(t, err, "creating volume in store returned an error")

	spec := volume.Spec.Copy()
	spec.Annotations.Labels = map[string]string{"foo": "bar"}

	_, err = ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
		VolumeID:      volume.ID,
		Spec:          spec,
		VolumeVersion: &api.Version{},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "update out of sequence")
}

// TestUpdateVolumeInvalidFields tests that updating several different fields
// in a volume is disallowed.
func TestUpdateVolumeInvalidFields(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	for _, tc := range []struct {
		name  string
		apply func(spec *api.VolumeSpec)
	}{
		{
			name: "Name",
			apply: func(spec *api.VolumeSpec) {
				spec.Annotations.Name = "someothername"
			},
		}, {
			name: "Group",
			apply: func(spec *api.VolumeSpec) {
				spec.Group = "adifferentgroup"
			},
		}, {
			name: "AccessibilityRequirements",
			apply: func(spec *api.VolumeSpec) {
				spec.AccessibilityRequirements = &api.TopologyRequirement{
					Requisite: []*api.Topology{
						{
							Segments: map[string]string{
								"com.mirantis/zone": "Z1",
							},
						},
					},
				}
			},
		}, {
			name: "Driver",
			apply: func(spec *api.VolumeSpec) {
				spec.Driver = &api.Driver{Name: "someotherdriver"}
			},
		}, {
			name: "AccessMode",
			apply: func(spec *api.VolumeSpec) {
				spec.AccessMode = &api.VolumeAccessMode{
					Scope:   api.VolumeScopeMultiNode,
					Sharing: api.VolumeSharingReadOnly,
				}
			},
		},
		// TODO(dperny): we should probably be able to update volume secrets.
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			// create a new volume for each iteration, so that tests are
			// independent
			volume := cannedVolume.Copy()
			volume.ID = fmt.Sprintf("testvolumeid%s", tc.name)
			volume.Spec.Annotations.Name = fmt.Sprintf("testvolumename%s", tc.name)
			// create a volume. avoid using CreateVolume, because we want to test
			// UpdateVolume in isolation
			err := ts.Store.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, volume)
			})
			assert.NoError(t, err, "creating volume in store returned an error")

			// we need to read the volume back out so we can get the current version
			// just reuse the volume variable and store the latest copy in it.
			ts.Store.View(func(tx store.ReadTx) {
				volume = store.GetVolume(tx, volume.ID)
			})

			spec := volume.Spec.Copy()
			tc.apply(spec)
			_, err = ts.Client.UpdateVolume(context.Background(), &api.UpdateVolumeRequest{
				VolumeID:      volume.ID,
				Spec:          spec,
				VolumeVersion: &volume.Meta.Version,
			})
			require.Error(t, err)
			assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))
			assert.Contains(t, err.Error(), tc.name)
		})
	}
}

func TestUpdateVolumeValidateSecrets(t *testing.T) {
	t.Skip("TODO: validate secrets on update")
}

func TestGetVolume(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	volume := cannedVolume.Copy()
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	})
	assert.NoError(t, err)

	resp, err := ts.Client.GetVolume(context.Background(), &api.GetVolumeRequest{
		VolumeID: volume.ID,
	})
	assert.NoError(t, err)
	require.NotNil(t, resp)
	require.NotNil(t, resp.Volume)
	require.Equal(t, resp.Volume, volume)
}

func TestGetVolumeNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.GetVolume(context.Background(), &api.GetVolumeRequest{
		VolumeID: "notreal",
	})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))
}

// TestListVolumesByDriver tests that filtering volumes based on volume group
// works correctly.
func TestListVolumesByGroup(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	alternativeDriver := "someOtherDriver"

	volumes := []*api.Volume{
		{
			ID: "volid0",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "invol0",
					Labels: map[string]string{
						"label1": "yes",
					},
				},
				Group: "group1",
				Driver: &api.Driver{
					Name: testVolumeDriver,
				},
			},
		}, {
			ID: "volid1",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "vol1",
					Labels: map[string]string{
						"label2": "no",
					},
				},
				Group: "group2",
				Driver: &api.Driver{
					Name: testVolumeDriver,
				},
			},
		}, {
			ID: "volid2",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "vol2",
					Labels: map[string]string{
						"label2": "yes",
					},
				},
				Group: "group1",
				Driver: &api.Driver{
					Name: testVolumeDriver,
				},
			},
		}, {
			ID: "volid3",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "vol3",
					Labels: map[string]string{
						"label1": "no",
					},
				},
				Group: "group3",
				Driver: &api.Driver{
					Name: alternativeDriver,
				},
			},
		}, {
			ID: "involid4",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "invol4",
					Labels: map[string]string{
						"label1": "yes",
						"label2": "no",
					},
				},
				Group: "group4",
				Driver: &api.Driver{
					Name: alternativeDriver,
				},
			},
		},
	}

	err := ts.Store.Update(func(tx store.Tx) error {
		for _, v := range volumes {
			if err := store.CreateVolume(tx, v); err != nil {
				return err
			}
		}
		return nil
	})

	assert.NoError(t, err)

	for _, tc := range []struct {
		name     string
		filters  *api.ListVolumesRequest_Filters
		expected []*api.Volume
	}{
		{
			name:     "All",
			expected: volumes,
		},
		{
			name: "FilterGroups",
			filters: &api.ListVolumesRequest_Filters{
				Groups: []string{"group1"},
			},
			expected: []*api.Volume{
				volumes[0], volumes[2],
			},
		}, {
			name: "FilterDrivers",
			filters: &api.ListVolumesRequest_Filters{
				Drivers: []string{alternativeDriver},
			},
			expected: []*api.Volume{
				volumes[3], volumes[4],
			},
		}, {
			name: "FilterName",
			filters: &api.ListVolumesRequest_Filters{
				Names: []string{"vol1", "invol4"},
			},
			expected: []*api.Volume{volumes[1], volumes[4]},
		}, {
			name: "FilterLabels",
			filters: &api.ListVolumesRequest_Filters{
				Labels: map[string]string{
					"label1": "yes",
				},
			},
			expected: []*api.Volume{volumes[0], volumes[4]},
		}, {
			name: "FilterIDPrefixes",
			filters: &api.ListVolumesRequest_Filters{
				IDPrefixes: []string{"volid"},
			},
			expected: []*api.Volume{
				volumes[0], volumes[1], volumes[2], volumes[3],
			},
		}, {
			name: "FilterNamePrefixes",
			filters: &api.ListVolumesRequest_Filters{
				NamePrefixes: []string{"invol"},
			},
			expected: []*api.Volume{
				volumes[0], volumes[4],
			},
		}, {
			name: "EmptyReturn",
			filters: &api.ListVolumesRequest_Filters{
				Names: []string{"notpresent"},
			},
			expected: []*api.Volume{},
		}, {
			name: "MixedFilters",
			filters: &api.ListVolumesRequest_Filters{
				IDPrefixes: []string{"vol"},
				Groups:     []string{"group1", "group3"},
				Labels: map[string]string{
					"label1": "",
				},
			},
			expected: []*api.Volume{
				volumes[0], volumes[3],
			},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			resp, err := ts.Client.ListVolumes(context.Background(), &api.ListVolumesRequest{
				Filters: tc.filters,
			})
			assert.NoError(t, err)
			require.NotNil(t, resp)
			assert.ElementsMatch(t, resp.Volumes, tc.expected)
		})
	}
}

// TestRemoveVolume tests that an unused volume can be removed successfully,
// meaning PendingDelete == true.
func TestRemoveVolume(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	require.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, cannedVolume)
	}))

	resp, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{
		VolumeID: cannedVolume.ID,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)

	var v *api.Volume
	ts.Store.View(func(tx store.ReadTx) {
		v = store.GetVolume(tx, cannedVolume.ID)
	})

	require.NotNil(t, v)
	assert.True(t, v.PendingDelete, "expected PendingDelete to be true")
}

// TestRemoveVolumeCreatedButNotInUse tests that a Volume which has passed
// through the creation stage and gotten a VolumeInfo, but is not published,
// can be successfully removed.
func TestRemoveVolumeCreatedButNotInUse(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()
	volume.VolumeInfo = &api.VolumeInfo{
		VolumeID: "csiID",
	}

	require.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	}))

	resp, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{
		VolumeID: volume.ID,
	})

	assert.NoError(t, err)
	assert.NotNil(t, resp)

	var v *api.Volume
	ts.Store.View(func(tx store.ReadTx) {
		v = store.GetVolume(tx, volume.ID)
	})

	require.NotNil(t, v)
	assert.True(t, v.PendingDelete, "expected PendingDelete to be true")
}

func TestRemoveVolumeInUse(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()
	volume.VolumeInfo = &api.VolumeInfo{
		VolumeID: "csiID",
	}
	volume.PublishStatus = []*api.VolumePublishStatus{
		{
			NodeID: "someNode",
			State:  api.VolumePublishStatus_PUBLISHED,
		},
	}

	require.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	}))

	resp, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{
		VolumeID: volume.ID,
	})

	assert.Error(t, err)
	assert.Equal(
		t, codes.FailedPrecondition, testutils.ErrorCode(err),
		"expected code FailedPrecondition",
	)
	assert.Nil(t, resp)

	var v *api.Volume
	ts.Store.View(func(tx store.ReadTx) {
		v = store.GetVolume(tx, volume.ID)
	})

	require.NotNil(t, v)
	require.False(t, v.PendingDelete, "expected PendingDelete to be false")
}

func TestRemoveVolumeNotFound(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	resp, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{
		VolumeID: "notReal",
	})

	assert.Error(t, err)
	assert.Equal(
		t, codes.NotFound, testutils.ErrorCode(err),
		"expected code NotFound",
	)
	assert.Nil(t, resp)
}

func TestRemoveVolumeForce(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	volume := cannedVolume.Copy()
	volume.VolumeInfo = &api.VolumeInfo{
		VolumeID: "csiID",
	}
	volume.PublishStatus = []*api.VolumePublishStatus{
		{
			NodeID: "someNode",
			State:  api.VolumePublishStatus_PUBLISHED,
		},
	}

	require.NoError(t, ts.Store.Update(func(tx store.Tx) error {
		return store.CreateVolume(tx, volume)
	}))

	_, err := ts.Client.RemoveVolume(context.Background(), &api.RemoveVolumeRequest{
		VolumeID: volume.ID,
		Force:    true,
	})

	assert.NoError(t, err)

	ts.Store.View(func(tx store.ReadTx) {
		v := store.GetVolume(tx, volume.ID)
		assert.Nil(t, v)
	})
}
