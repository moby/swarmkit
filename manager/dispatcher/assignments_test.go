package dispatcher

import (
	"testing"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/manager/state/testutils"
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
		}

		return store.CreateVolume(tx, v)
	}))

	// next, create an assignment set
	as := newAssignmentSet(
		// we'll just use the standard logger here. it's as good as any other.
		logrus.NewEntry(logrus.StandardLogger()),
		// we can pass nil for the DriverProvider here, because it only comes
		// into play if the secret's driver is non-nil.
		nil,
	)

	mapKey := typeAndID{objType: api.ResourceType_VOLUME, id: "volume1"}

	// we need a task here, because in assignVolume we call assignSecret, which
	// needs a task object. fortunately, it can be pretty bare.
	task := &api.Task{
		ID: "task1",
	}

	// finally, we can do our assignVolume call
	s.View(func(tx store.ReadTx) {
		assignVolume(as, tx, mapKey, task)
	})

	// ensure that the assignments match what we expect
	assert.Len(t, as.changes, 3)

	var foundSecret1, foundSecret2 bool
	for _, change := range as.changes {
		// every one of these should be an Update change
		assert.Equal(t, change.Action, api.AssignmentChange_AssignmentActionUpdate)
		switch c := change.Assignment.Item.(type) {
		case *api.Assignment_Volume:
			assert.Equal(t, c.Volume, &api.VolumeAssignment{
				ID:       "volume1",
				VolumeID: "volumeID1",
				Driver: &api.Driver{
					Name: "driver",
				},
				VolumeContext:  map[string]string{"foo": "bar"},
				PublishContext: map[string]string{},
				Secrets: []*api.VolumeSecret{
					{Key: "secretKey1", Secret: "secret1"},
					{Key: "secretKey2", Secret: "secret2"},
				},
			})
		case *api.Assignment_Secret:
			// we don't need to test correctness of the assignment content,
			// just that it's present
			switch c.Secret.ID {
			case "secret1":
				foundSecret1 = true
			case "secret2":
				foundSecret2 = true
			default:
				t.Fatalf("found unexpected secret assignment %s", c.Secret.ID)
			}

		default:
			t.Fatalf("found an unexpected assignment, type %T", c)
		}
	}

	assert.True(t, foundSecret1, "expected to find secret1 assignment")
	assert.True(t, foundSecret2, "expected to find secret2 assignment")
}
