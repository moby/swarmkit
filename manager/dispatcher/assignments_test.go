package dispatcher

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/manager/state/testutils"
)

func TestAssignVolume(t *testing.T) {
	// first, create the store and populate it with the volume and some secrets
	s := store.NewMemoryStore(&testutils.MockProposer{})
	require.NoError(t, s.Update(func(tx store.Tx) error {
		for _, s := range []*api.Secret{
			{
				ID: "secret1",
				Spec: api.SecretSpec{
					Annotations: api.Annotations{
						Name: "secretName1",
					},
					Data: []byte("foo"),
				},
			}, {
				ID: "secret2",
				Spec: api.SecretSpec{
					Annotations: api.Annotations{
						Name: "secretName2",
					},
					Data: []byte("foo"),
				},
			},
		} {
			if err := store.CreateSecret(tx, s); err != nil {
				return err
			}
		}

		v := &api.Volume{
			ID: "volume1",
			Spec: api.VolumeSpec{
				Annotations: api.Annotations{
					Name: "volumeName1",
				},
				Driver: &api.Driver{
					Name: "driver",
				},
				Secrets: []*api.VolumeSecret{
					{
						Key:    "secretKey1",
						Secret: "secret1",
					}, {
						Key:    "secretKey2",
						Secret: "secret2",
					},
				},
			},
			VolumeInfo: &api.VolumeInfo{
				VolumeContext: map[string]string{"foo": "bar"},
				VolumeID:      "volumeID1",
			},
			PublishStatus: []*api.VolumePublishStatus{
				{
					NodeID: "node1",
					State:  api.VolumePublishStatus_PENDING_PUBLISH,
				},
				{
					NodeID:         "nodeNotThisOne",
					State:          api.VolumePublishStatus_PUBLISHED,
					PublishContext: map[string]string{"shouldnot": "bethisone"},
				},
			},
		}

		return store.CreateVolume(tx, v)
	}))

	// next, create an assignment set
	as := newAssignmentSet(
		"node1",
		// we'll just use the standard logger here. it's as good as any other.
		logrus.NewEntry(logrus.StandardLogger()),
		// we can pass nil for the DriverProvider here, because it only comes
		// into play if the secret's driver is non-nil.
		nil,
	)

	var modified bool

	// finally, we can do our assignVolume call
	s.View(func(tx store.ReadTx) {
		v := store.GetVolume(tx, "volume1")
		modified = as.addOrUpdateVolume(tx, v)
	})

	assert.False(t, modified)

	// calling as.message gets the changes until now and also reset the changes
	// tracked internally.
	m := as.message()

	// the Volume will not yet be assigned, because it is not ready.
	assert.Empty(t, m.Changes)

	var (
		nv *api.Volume
	)

	// now update the volume to state PUBLISHED
	require.NoError(t, s.Update(func(tx store.Tx) error {
		nv = store.GetVolume(tx, "volume1")
		for _, s := range nv.PublishStatus {
			if s.NodeID == "node1" {
				s.State = api.VolumePublishStatus_PUBLISHED
				s.PublishContext = map[string]string{
					"shouldbe": "thisone",
				}
				break
			}
		}
		return store.UpdateVolume(tx, nv)
	}))

	s.View(func(tx store.ReadTx) {
		vol := store.GetVolume(tx, "volume1")
		modified = as.addOrUpdateVolume(tx, vol)
	})

	assert.True(t, modified)

	m = as.message()
	assert.Len(t, m.Changes, 3)

	var foundSecret1, foundSecret2 bool
	for _, change := range m.Changes {
		if vol, ok := change.Assignment.Item.(*api.Assignment_Volume); ok {
			assert.Equal(t, api.AssignmentChange_AssignmentActionUpdate, change.Action)
			assert.Equal(t, &api.VolumeAssignment{
				ID:       "volume1",
				VolumeID: "volumeID1",
				Driver: &api.Driver{
					Name: "driver",
				},
				VolumeContext:  map[string]string{"foo": "bar"},
				PublishContext: map[string]string{"shouldbe": "thisone"},
				Secrets: []*api.VolumeSecret{
					{Key: "secretKey1", Secret: "secret1"},
					{Key: "secretKey2", Secret: "secret2"},
				},
			}, vol.Volume)
		} else {
			secretAssignment := change.Assignment.Item.(*api.Assignment_Secret)
			// we don't need to test correctness of the assignment content,
			// just that it's present
			switch secretAssignment.Secret.ID {
			case "secret1":
				foundSecret1 = true
			case "secret2":
				foundSecret2 = true
			default:
				t.Fatalf("found unexpected secret assignment %s", secretAssignment.Secret.ID)
			}
		}

		// every one of these should be an Update change
		assert.Equal(t, api.AssignmentChange_AssignmentActionUpdate, change.Action)
	}

	assert.True(t, foundSecret1)
	assert.True(t, foundSecret2)

	// now update the volume to be pending removal
	require.NoError(t, s.Update(func(tx store.Tx) error {
		v := store.GetVolume(tx, "volume1")
		for _, status := range v.PublishStatus {
			if status.NodeID == "node1" {
				status.State = api.VolumePublishStatus_PENDING_NODE_UNPUBLISH
			}
		}

		return store.UpdateVolume(tx, v)
	}))

	s.View(func(tx store.ReadTx) {
		v := store.GetVolume(tx, "volume1")
		modified = as.addOrUpdateVolume(tx, v)
	})
	assert.True(t, modified)

	m = as.message()
	assert.Len(t, m.Changes, 1)

	assert.Equal(t, api.AssignmentChange_AssignmentActionRemove, m.Changes[0].Action)
	v, ok := m.Changes[0].Assignment.Item.(*api.Assignment_Volume)
	assert.True(t, ok)
	assert.Equal(t, &api.VolumeAssignment{
		ID:       "volume1",
		VolumeID: "volumeID1",
		Driver: &api.Driver{
			Name: "driver",
		},
		VolumeContext:  map[string]string{"foo": "bar"},
		PublishContext: map[string]string{"shouldbe": "thisone"},
		Secrets: []*api.VolumeSecret{
			{Key: "secretKey1", Secret: "secret1"},
			{Key: "secretKey2", Secret: "secret2"},
		},
	}, v.Volume)

	// now update the volume again, this time, to acknowledge its removal on
	// the node.
	require.NoError(t, s.Update(func(tx store.Tx) error {
		v := store.GetVolume(tx, "volume1")
		for _, status := range v.PublishStatus {
			if status.NodeID == "node1" {
				status.State = api.VolumePublishStatus_PENDING_UNPUBLISH
			}
		}
		return store.UpdateVolume(tx, v)
	}))

	s.View(func(tx store.ReadTx) {
		v := store.GetVolume(tx, "volume1")
		modified = as.addOrUpdateVolume(tx, v)
	})
	assert.True(t, modified)
	m = as.message()
	assert.Len(t, m.Changes, 2)
	foundSecret1 = false
	foundSecret2 = false

	for _, change := range m.Changes {
		assert.Equal(t, api.AssignmentChange_AssignmentActionRemove, change.Action)
		s, ok := change.Assignment.Item.(*api.Assignment_Secret)
		assert.True(t, ok)
		switch s.Secret.ID {
		case "secret1":
			foundSecret1 = true
		case "secret2":
			foundSecret2 = true
		default:
			t.Fatalf("found unexpected secret assignment %s", s.Secret.ID)
		}
	}
}
