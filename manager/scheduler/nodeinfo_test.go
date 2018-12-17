package scheduler

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/genericresource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemoveTask(t *testing.T) {
	nodeResourceSpec := &api.Resources{
		NanoCPUs:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red", "green"),
			genericresource.NewDiscrete("apple", 6),
		),
	}

	node := &api.Node{
		Description: &api.NodeDescription{Resources: nodeResourceSpec},
	}

	tasks := map[string]*api.Task{
		"task1": {
			ID: "task1",
		},
		"task2": {
			ID: "task2",
		},
	}

	available := api.Resources{
		NanoCPUs:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red"),
			genericresource.NewDiscrete("apple", 5),
		),
	}

	taskRes := &api.Resources{
		NanoCPUs:    5000,
		MemoryBytes: 5000,
		Generic: []*api.GenericResource{
			genericresource.NewDiscrete("apple", 1),
			genericresource.NewDiscrete("orange", 1),
		},
	}

	task1 := &api.Task{
		ID: "task1",
		Spec: api.TaskSpec{
			Resources: &api.ResourceRequirements{Reservations: taskRes},
		},
		AssignedGenericResources: append(
			genericresource.NewSet("orange", "green"),
			genericresource.NewDiscrete("apple", 1),
		),
	}

	task3 := &api.Task{
		ID: "task3",
	}

	// nodeInfo has no tasks
	nodeInfo := newNodeInfo(node, nil, available)
	assert.False(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has taskID
	nodeInfo = newNodeInfo(node, tasks, available)
	assert.True(t, nodeInfo.removeTask(task1))

	// nodeInfo's tasks has no taskID
	assert.False(t, nodeInfo.removeTask(task3))

	nodeAvailableResources := nodeInfo.AvailableResources

	cpuLeft := available.NanoCPUs + taskRes.NanoCPUs
	memoryLeft := available.MemoryBytes + taskRes.MemoryBytes

	assert.Equal(t, cpuLeft, nodeAvailableResources.NanoCPUs)
	assert.Equal(t, memoryLeft, nodeAvailableResources.MemoryBytes)

	assert.Equal(t, 4, len(nodeAvailableResources.Generic))

	apples := genericresource.GetResource("apple", nodeAvailableResources.Generic)
	oranges := genericresource.GetResource("orange", nodeAvailableResources.Generic)
	assert.Len(t, apples, 1)
	assert.Len(t, oranges, 3)

	for _, k := range []string{"red", "blue", "green"} {
		assert.True(t, genericresource.HasResource(
			genericresource.NewString("orange", k), oranges),
		)
	}

	assert.Equal(t, int64(6), apples[0].GetDiscreteResourceSpec().Value)
}

func TestAddTask(t *testing.T) {
	node := &api.Node{}

	tasks := map[string]*api.Task{
		"task1": {
			ID: "task1",
		},
		"task2": {
			ID: "task2",
		},
	}

	task1 := &api.Task{
		ID: "task1",
	}

	available := api.Resources{
		NanoCPUs:    100000,
		MemoryBytes: 1000000,
		Generic: append(
			genericresource.NewSet("orange", "blue", "red"),
			genericresource.NewDiscrete("apple", 5),
		),
	}

	taskRes := &api.Resources{
		NanoCPUs:    5000,
		MemoryBytes: 5000,
		Generic: []*api.GenericResource{
			genericresource.NewDiscrete("apple", 2),
			genericresource.NewDiscrete("orange", 1),
		},
	}

	task3 := &api.Task{
		ID: "task3",
		Spec: api.TaskSpec{
			Resources: &api.ResourceRequirements{Reservations: taskRes},
		},
	}

	nodeInfo := newNodeInfo(node, tasks, available)

	// add task with ID existing
	assert.False(t, nodeInfo.addTask(task1))

	// add task with ID non-existing
	assert.True(t, nodeInfo.addTask(task3))

	// add again
	assert.False(t, nodeInfo.addTask(task3))

	// Check resource consumption of node
	nodeAvailableResources := nodeInfo.AvailableResources

	cpuLeft := available.NanoCPUs - taskRes.NanoCPUs
	memoryLeft := available.MemoryBytes - taskRes.MemoryBytes

	assert.Equal(t, cpuLeft, nodeAvailableResources.NanoCPUs)
	assert.Equal(t, memoryLeft, nodeAvailableResources.MemoryBytes)

	apples := genericresource.GetResource("apple", nodeAvailableResources.Generic)
	oranges := genericresource.GetResource("orange", nodeAvailableResources.Generic)
	assert.Len(t, apples, 1)
	assert.Len(t, oranges, 1)

	o := oranges[0].GetNamedResourceSpec()
	assert.True(t, o.Value == "blue" || o.Value == "red")
	assert.Equal(t, int64(3), apples[0].GetDiscreteResourceSpec().Value)

}

// TestNewNodeInfoWithDevices tests that calling newNodeInfo correctly
// populates the local state of which devices are available and in use on the
// node.
//
// The scheduler doesn't actually look at the store for a DeviceClass. The
// controlapi is in charge of enforcing the existence of device classes and
// otherwise general correctness of a Device before a Device can be added. In
// the scheduler, we can trust that the controlapi has done this correctly and
// impute the existence of device classes by looking at what classes the
// devices on nodes belong to. For this test, we're using two device classes:
// FooDev and BarDev.
func TestNewNodeInfoWithDevices(t *testing.T) {
	// we don't actually care about node resources in this test, so we're gonna
	// create one empty api.Resources object
	res := api.Resources{}

	// this first node will be created with no preassigned tasks. it has 3
	// devices: one FooDev, one BarDev, and 1 device that belongs to both.
	node := &api.Node{
		ID: "node",
		Spec: api.NodeSpec{
			Devices: []*api.Device{
				&api.Device{
					DeviceClassID: "FooClass",
					Path:          "/dev/foo1",
				},
				&api.Device{
					DeviceClassID: "BarClass",
					Path:          "/dev/bar1",
				},
				&api.Device{
					DeviceClassID: "FooClass",
					Path:          "/dev/foobar",
				},
				&api.Device{
					DeviceClassID: "BarClass",
					Path:          "/dev/foobar",
				},
			},
		},
	}

	nodeInfo := newNodeInfo(node, map[string]*api.Task{}, res)
	for class, devices := range map[string][]string{
		"FooClass": []string{"/dev/foo1", "/dev/foobar"},
		"BarClass": []string{"/dev/bar1", "/dev/foobar"},
	} {
		// NodeInfo keeps track of the devices on a node through two maps.
		// The first, nodeDeviceClasses, contains a mapping of the ID of a
		// DeviceClass to the devices on a node that belong to that class. The
		// second, nodeDevices, contains a mapping of the devices available on
		// the node to the task that is using them.
		set, exists := nodeInfo.nodeDeviceClasses[class]
		// first, we need to be sure that the nodeDeviceClasses set exists and
		// contains the correct devices.  we're using require here, because
		// otherwise if this check were to fail, then the next bit of code
		// would try to access a nil map and panic
		require.True(t, exists, "NodeInfo should have device class %v", class)
		require.NotNil(
			t, devices, "device set for device class %v should not be nil", class,
		)

		// now verify that the devices set includes all devices in the class.
		for _, dev := range devices {
			assert.Contains(
				t, set, dev, "expected set for class %v to contain device %v",
				class, dev,
			)
		}
	}

	// finally, assert that there is a device tracking entry in the NodeInfo
	// for each device, and that the current value is emptystring, meaning the
	// device is free
	for _, dev := range []string{"/dev/foo1", "/dev/bar1", "/dev/foobar"} {
		value, exists := nodeInfo.nodeDevices[dev]
		assert.True(t, exists, "expected nodeDevices to include device %v")
		assert.Equal(
			t, value, "",
			"expected nodeDevices entry for device %v to be emptystring", dev,
		)
	}
}

// TestNewNodeInfoWithDevicesAndTasks checks the same stuff as
// TestNewNodeInfoWithDevices, except it also verifies that if preassigned
// tasks are provided, the local state is correctly populated with those tasks.
func TestNewNodeInfoWithDevicesAndTasks(t *testing.T) {
	// we don't actually care about node resources in this test, so we're gonna
	// create one empty api.Resources object
	res := api.Resources{}

	// this first node will be created with no preassigned tasks. it has 3
	// devices: one FooDev, two BarDev, and 1 device that belongs to both.
	node := &api.Node{
		ID: "node",
		Spec: api.NodeSpec{
			Devices: []*api.Device{
				&api.Device{
					DeviceClassID: "FooClass",
					Path:          "/dev/foo1",
				},
				&api.Device{
					DeviceClassID: "BarClass",
					Path:          "/dev/bar1",
				},
				&api.Device{
					DeviceClassID: "BarClass",
					Path:          "/dev/bar2",
				},
				&api.Device{
					DeviceClassID: "FooClass",
					Path:          "/dev/foobar",
				},
				&api.Device{
					DeviceClassID: "BarClass",
					Path:          "/dev/foobar",
				},
			},
		},
	}

	// task1 uses device /dev/foobar
	task1 := &api.Task{
		ID:     "task1",
		NodeID: "node",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "FooClass",
					Path:          "/dev/whatever",
				},
			},
		},
		Devices: []*api.DeviceAttachment{
			&api.DeviceAttachment{
				DeviceClassID: "FooClass",
				PathOnHost:    "/dev/foobar",
				PathInTask:    "/dev/whatever",
			},
		},
	}

	// task 2 uses device /dev/bar1 and /dev/bar2
	task2 := &api.Task{
		ID:     "task2",
		NodeID: "node",
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "BarClass",
					Path:          "/dev/spot1",
				},
				&api.DeviceAttachmentSpec{
					DeviceClassID: "BarClass",
					Path:          "/dev/spot2",
				},
			},
		},
		Devices: []*api.DeviceAttachment{
			&api.DeviceAttachment{
				DeviceClassID: "BarClass",
				PathInTask:    "/dev/spot1",
				PathOnHost:    "/dev/bar2",
			},
			&api.DeviceAttachment{
				DeviceClassID: "BarClass",
				PathInTask:    "/dev/spot2",
				PathOnHost:    "/dev/bar1",
			},
		},
	}

	// now, put these tasks in a map to be consumed by newNodeInfo
	tasks := map[string]*api.Task{"task1": task1, "task2": task2}

	// and create the NodeInfo object
	nodeInfo := newNodeInfo(node, tasks, res)

	// we're not going to check that the nodeDeviceClasses has been initialized
	// correctly; TestNewNodeInfoWithDevices covers that case. we're only going
	// to test that nodeDevices has the correct assignments
	for dev, task := range map[string]string{
		"/dev/foobar": "task1",
		"/dev/bar1":   "task2",
		"/dev/bar2":   "task2",
		"/dev/foo1":   "",
	} {
		assignedTo, exists := nodeInfo.nodeDevices[dev]
		assert.True(t, exists, "expected node to have device %v")
		assert.Equal(t, assignedTo, task)
	}
}

// TestAddTaskWithDevices tests that adding a task which requires devices
// correctly marks those devices as in use by the task, and correctly assigns
// them to the task.
func TestAddTaskWithDevices(t *testing.T) {
	// first, we'll need a node that we're adding devices to. this node will
	// have these devices:
	//   - /dev/foo1 is a FooDev device
	//   - /dev/bar1 is a BarDev device
	//   - /dev/foobar is both a FooDev and BarDev device
	node := &api.Node{
		ID: "testDevicesNode1",
		Spec: api.NodeSpec{
			Devices: []*api.Device{
				&api.Device{
					DeviceClassID: "FooDev",
					Path:          "/dev/foo1",
				},
				&api.Device{
					DeviceClassID: "BarDev",
					Path:          "/dev/bar1",
				},
				&api.Device{
					DeviceClassID: "FooDev",
					Path:          "/dev/foobar",
				},
				&api.Device{
					DeviceClassID: "BarDev",
					Path:          "/dev/foobar",
				},
			},
		},
	}

	// TODO(dperny): ah crap I just realized that it's possible for a task to
	// pass the filter but fail the assignment if it's supposed to use a task
	// supporting multiple devices.

	// now create the NodeInfo object
	nodeInfo := newNodeInfo(node, nil, api.Resources{})

	// now, let's add a task to the node
	task1 := &api.Task{
		ID: "testDevicesTask1",
		// assign this task to the node. I don't think this makes a difference
		// but it's better to be clear
		NodeID: node.ID,
		Spec: api.TaskSpec{
			Devices: []*api.DeviceAttachmentSpec{
				&api.DeviceAttachmentSpec{
					DeviceClassID: "FooDev",
					Path:          "/dev/foo",
				},
				&api.DeviceAttachmentSpec{
					DeviceClassID: "BarDev",
					Path:          "/dev/bar",
				},
			},
		},
	}

	// add the task to the node
	res := nodeInfo.addTask(task1)
	assert.True(t, res, "adding task 1 should have modified nodeInfo")
	assert.Len(t, task1.Devices, 2, "task 1 should have 2 devices")

	var numFooDev, numBarDev int
	for _, dev := range task1.Devices {
		if dev.DeviceClassID == "FooDev" {
			numFooDev++
		}
		if dev.DeviceClassID == "BarDev" {
			numBarDev++
		}
		t.Logf(
			"task is using %v to fulfil device class %v",
			dev.PathOnHost, dev.DeviceClassID,
		)
	}

	assert.Equal(t, numFooDev, 1, "task 1 should have 1 FooDev device")
	assert.Equal(t, numBarDev, 1, "task 1 should have 1 BarDev device")
}
