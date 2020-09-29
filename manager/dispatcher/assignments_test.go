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
		ID:     "task1",
		NodeID: "node1",
	}

	// finally, we can do our assignVolume call
	s.View(func(tx store.ReadTx) {
		assignVolume(as, tx, mapKey, task)
	})

	// calling as.message gets the changes until now and also reset the changes
	// tracked internally.
	m := as.message()

	// the assignVolume call won't actually assign the volume, because it is not
	// ready. instead, we expect it to be put in pendingVolumes. therefore,
	// there should be 2 changes, both volume secrets.
	assert.Len(t, m.Changes, 2)

	var foundSecret1, foundSecret2 bool
	for _, change := range m.Changes {
		// every one of these should be an Update change
		assert.Equal(t, change.Action, api.AssignmentChange_AssignmentActionUpdate)
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
	/*
		case *api.Assignment_Volume:
			assert.Equal(t, c.Volume, &api.VolumeAssignment{
			})
	*/

	assert.True(t, foundSecret1, "expected to find secret1 assignment")
	assert.True(t, foundSecret2, "expected to find secret2 assignment")

	var (
		nv              *api.Volume
		thisNodePublish *api.VolumePublishStatus
	)

	// now update the volume to state PUBLISHED
	require.NoError(t, s.Update(func(tx store.Tx) error {
		nv = store.GetVolume(tx, "volume1")
		for _, s := range nv.PublishStatus {
			if s.NodeID == "node1" {
				thisNodePublish = s
				s.State = api.VolumePublishStatus_PUBLISHED
				s.PublishContext = map[string]string{
					"shouldbe": "thisone",
				}
				break
			}
		}
		return store.UpdateVolume(tx, nv)
	}))

	// now we should call sendVolume
	as.sendVolume("volume1", thisNodePublish)

	m2 := as.message()
	require.Len(t, m2.Changes, 1)
	av, ok := m2.Changes[0].Assignment.Item.(*api.Assignment_Volume)
	require.True(t, ok, "expected assignment to be of type Assignment_Volume")

	assert.Equal(t, av.Volume, &api.VolumeAssignment{
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
	})
}
