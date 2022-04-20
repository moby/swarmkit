package csi

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/agent/exec"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/volumequeue"
	"github.com/stretchr/testify/assert"
)

const (
	driver = "driver"

	iterations = 25
	interval   = 100 * time.Millisecond
)

func NewFakeManager() *volumes {
	// with fakes, we don't need to actually USE plugingetter, because it's
	// only used by the plugin manager, which we are faking. instead, manually
	// set the enabled plugins
	pm := newFakePluginManager()
	pm.plugins[driver] = newFakeNodePlugin(driver)

	return &volumes{
		volumes:        map[string]volumeState{},
		pendingVolumes: volumequeue.NewVolumeQueue(),
		plugins:        pm,
	}
}

func TestTaskRestrictedVolumesProvider(t *testing.T) {
	taskID := "taskID1"
	type testCase struct {
		desc        string
		volumes     exec.VolumeGetter
		volumeID    string
		expectedErr string
	}

	testCases := []testCase{
		// The default case when not using a volumes driver or not returning.
		// Test to check if volume ID is allowed to access
		{
			desc:     "AllowedVolume",
			volumeID: "volume1",
		},
		// Test to check if volume ID is not allowed to access
		{
			desc:        "RestrictedVolume",
			expectedErr: fmt.Sprintf("task not authorized to access volume volume2"),
			volumeID:    "volume2",
		},
	}

	for _, testCase := range testCases {
		testCase := testCase
		t.Run(testCase.desc, func(t *testing.T) {
			ctx := context.Background()

			// create a new volumesManager each test.
			volumesManager := NewFakeManager()

			v := api.VolumeAssignment{
				ID:     testCase.volumeID,
				Driver: &api.Driver{Name: driver},
			}

			volumesManager.Add(v)
			volumesManager.pendingVolumes.Wait()
			volumesManager.tryVolume(ctx, v.ID, 0)

			volumesGetter := Restrict(volumesManager, &api.Task{
				ID: taskID,
				Volumes: []*api.VolumeAttachment{
					{
						ID: "volume1",
					},
				},
			})

			volume, err := volumesGetter.Get(testCase.volumeID)
			if testCase.expectedErr != "" {
				assert.Error(t, err, testCase.desc)
				assert.Equal(t, testCase.expectedErr, err.Error())
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, volume)
			}
		})
	}
}
