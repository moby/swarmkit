package scheduler

import (
	"testing"

	"github.com/docker/swarmkit/api"

	"github.com/stretchr/testify/assert"
)

// TestDeviceFilterSetTask tests the SetTask method of a DeviceFilter. It
// checks two scenarios:
//   1. a task which requires no devices does not need the DeviceFilter
//      so SetTask returns `false`
//   2. a task which requires devices needs the DeviceFilter so SetTask
//      returns `true`.
//   3. a task which requires devices is set as the task field in the
//      DeviceFilter object
func TestDeviceFilterSetTask(t *testing.T) {
	// create the device filter we'll use for this test
	f := &DeviceFilter{}

	// two tasks with variable names that make their purpose very clear
	taskNoDevices := &api.Task{
		ID:   "taskNoDevices",
		Spec: api.TaskSpec{},
	}
	taskWithDevices := &api.Task{
		ID: "taskWithDevices",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "someClass",
					Path:          "/dev/foo",
				},
			},
		},
	}

	assert.False(
		t, f.SetTask(taskNoDevices),
		"task with no devices should not need DeviceFilter",
	)

	assert.True(
		t, f.SetTask(taskWithDevices),
		"task with devices should need the DeviceFilter",
	)

	assert.Equal(
		t, f.task, taskWithDevices,
		"task with devices should be set a (*DeviceFilter).task",
	)
}

// TestDeviceFilterCheck tests that the Check method of the DeviceFilter
// correctly identifies nodes which match task device requirements. This is
// very much a whitebox test which relies on the internals of NodeInfo. It is
// done this way to avoid creating a larger test footprintj. It tests these
// scenarios:
//   1. for a task which requests 1 device:
//      a. a node which has 1 of that device free passes the filter
//      b. a node does not have that device fails the filter.
//      c. a node which has 1 of that device, but already in use, fails the
//         filter
//      d. a node which has 2 of that device, but with 1 already in use, passes
//         the filter
//   2. for a task which requests 2 different devices:
//      a. a node which has both of those devices free passes the filter.
//      b. a node which has one of those devices but not the other fails the
//         filter.
//      c. a node which has both of those devices but neither is free fails the
//         filter
//      d. a node which one device that matches both requested devices fails
//         the filter
//   3. for a task which requests 2 of the same device:
//      a. node which has 2 of that device free passes the filter
//      b. a node which has 1 of that device fails the filter.
//      c. a node which has 2 of that device, but 1 already in use, fails the
//         filter
func TestDeviceFilterCheck(t *testing.T) {
	// all we need for this test is the nodeDevices and nodeDeviceClasses
	// fields of the NodeInfo object filled in
	// we're going to use 2 DeviceClasses for this test: FooDev and BarDev

	// nodeInfo1 has no devices on it
	nodeInfo1 := &NodeInfo{}

	// nodeInfo2 has 1 FooDev device at /dev/foo
	nodeInfo2 := &NodeInfo{
		nodeDevices: map[string]string{"/dev/foo": ""},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{"/dev/foo": struct{}{}},
		},
	}

	// nodeInfo3 has 1 FooDev device at /dev/foo, but it is already in use
	nodeInfo3 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foo": "someTask",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{"/dev/foo": struct{}{}},
		},
	}

	// nodeInfo4 has 2 FooDev devices, and one of them is in use
	nodeInfo4 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foo1": "someTask",
			"/dev/foo2": "",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{
				"/dev/foo1": struct{}{},
				"/dev/foo2": struct{}{},
			},
		},
	}

	// we'll of course need a DeviceFilter to test
	f := &DeviceFilter{}

	// now, test the first set of scenarios: a task which request 1 device
	f.SetTask(&api.Task{
		ID: "task1",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{DeviceClassID: "FooDev", Path: "/dev/whocares"},
			},
		},
	})

	assert.False(t, f.Check(nodeInfo1), "node 1 should fail check because it has no devices")
	assert.True(t, f.Check(nodeInfo2), "node 2 should pass check because it has a free device")
	assert.False(t, f.Check(nodeInfo3), "node 3 should fail because it has the device, but it is not free")
	assert.True(t, f.Check(nodeInfo4), "node 4 should pass the check because it has a free device")

	// now the second set of scenarios: a task requests 2 different devices

	// node 5 has both of these devices free
	nodeInfo5 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foo": "",
			"/dev/bar": "",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{"/dev/foo": struct{}{}},
			"BarDev": map[string]struct{}{"/dev/bar": struct{}{}},
		},
	}

	// node 6 has both of these devices, but neither is free
	nodeInfo6 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foo": "someTask",
			"/dev/bar": "someTask",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{"/dev/foo": struct{}{}},
			"BarDev": map[string]struct{}{"/dev/bar": struct{}{}},
		},
	}

	// node 7 has one device, /dev/foobar, which is in both classes
	nodeInfo7 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foobar": "",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{"/dev/foobar": struct{}{}},
			"BarDev": map[string]struct{}{"/dev/foobar": struct{}{}},
		},
	}

	f.SetTask(&api.Task{
		ID: "task2",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "FooDev",
					Path:          "/dev/whocares",
				},
				&api.DeviceAttachmentSpec{
					DeviceClassID: "BarDev",
					Path:          "/dev/whocares2",
				},
			},
		},
	})

	assert.True(t, f.Check(nodeInfo5), "node 5 should pass because it has both devices available")
	assert.False(t, f.Check(nodeInfo2), "node 2 should fail because it only has a FooDev device")
	assert.False(t, f.Check(nodeInfo6), "node 6 should fail because its devices are in use")
	assert.False(t, f.Check(nodeInfo7), "node 7 should fail because it only has 1 device that belongs to 2 classes")

	f.SetTask(&api.Task{
		ID: "task3",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "FooDev",
					Path:          "/dev/whocares",
				},
				&api.DeviceAttachmentSpec{
					DeviceClassID: "FooDev",
					Path:          "/dev/whocares2",
				},
			},
		},
	})

	//  final set of scenarios: the task request 2 FooDev devices
	nodeInfo8 := &NodeInfo{
		nodeDevices: map[string]string{
			"/dev/foo1": "",
			"/dev/foo2": "",
		},
		nodeDeviceClasses: map[string]map[string]struct{}{
			"FooDev": map[string]struct{}{
				"/dev/foo1": struct{}{},
				"/dev/foo2": struct{}{},
			},
		},
	}

	assert.True(t, f.Check(nodeInfo8), "node 8 should pass because it has 2 of this device free")
	assert.False(t, f.Check(nodeInfo2), "node 2 should fail because it only has 1 of this device")
	assert.False(t, f.Check(nodeInfo4), "node 4 should fail because it has 2 of this device, but only 1 is free")
}
