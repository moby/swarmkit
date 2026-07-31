package constraintenforcer

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/api/genericresource"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRejectNoncompliantTasksIgnoresCompletedJobTasksInReservations(t *testing.T) {
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	t.Cleanup(func() { _ = s.Close() })

	node := &api.Node{
		Id: "node1",
		Spec: &api.NodeSpec{
			Availability: api.NodeSpec_ACTIVE,
		},
		Description: &api.NodeDescription{
			Resources: &api.Resources{
				MemoryBytes: 1024,
			},
		},
	}

	// A live task that fits if completed job tasks are ignored.
	runningTask := &api.Task{
		Id:           "running1",
		NodeId:       node.Id,
		ServiceId:    "svc1",
		DesiredState: api.TaskState_RUNNING,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{
				Reservations: &api.Resources{
					MemoryBytes: 700,
				},
			},
		},
	}

	// A completed replicated-job task that should not consume reservations.
	completedJobTask := &api.Task{
		Id:           "job1",
		NodeId:       node.Id,
		ServiceId:    "jobsvc",
		DesiredState: api.TaskState_COMPLETE,
		Status: &api.TaskStatus{
			State: api.TaskState_COMPLETE,
		},
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{
				Reservations: &api.Resources{
					MemoryBytes: 700,
				},
			},
		},
	}

	require.NoError(t, s.Update(func(tx store.Tx) error {
		if err := store.CreateNode(tx, node); err != nil {
			return err
		}
		if err := store.CreateTask(tx, runningTask); err != nil {
			return err
		}
		if err := store.CreateTask(tx, completedJobTask); err != nil {
			return err
		}
		return nil
	}))

	ce := New(s)
	ce.rejectNoncompliantTasks(node)

	s.View(func(tx store.ReadTx) {
		got := store.GetTask(tx, runningTask.Id)
		require.NotNil(t, got)
		assert.NotEqual(t, api.TaskState_REJECTED, got.Status.GetState(), "running task unexpectedly rejected; completed job tasks should not count toward reservations")
	})
}

func TestConstraintEnforcer(t *testing.T) {
	nodes := []*api.Node{
		// this node starts as a worker, but then is changed to a manager.
		{
			Id: "id1",
			Spec: &api.NodeSpec{
				Annotations: &api.Annotations{
					Name: "name1",
				},
				Availability: api.NodeSpec_ACTIVE,
			},
			Status: &api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Role: api.NodeRole_WORKER,
		},
		{
			Id: "id2",
			Spec: &api.NodeSpec{
				Annotations: &api.Annotations{
					Name: "name2",
				},
				Availability: api.NodeSpec_ACTIVE,
			},
			Status: &api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Description: &api.NodeDescription{
				Resources: &api.Resources{
					NanoCpus:    1e9,
					MemoryBytes: 1e9,
				},
			},
		},
	}

	tasks := []*api.Task{
		// This task should not run, because id1 is a worker
		{
			Id:           "id0",
			DesiredState: api.TaskState_RUNNING,
			Spec: &api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{"node.role == manager"},
				},
			},
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			NodeId: "id1",
		},
		// this task should run without question
		{
			Id:           "id1",
			DesiredState: api.TaskState_RUNNING,
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			NodeId: "id1",
		},
		// this task, which might belong to a job, should run.
		{
			Id:           "id5",
			DesiredState: api.TaskState_COMPLETE,
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			NodeId: "id1",
		},
		// this task should run fine and not shut down at first, because node
		// id1 is correctly a worker. but when the node is updated to be a
		// manager, it should be rejected
		{
			Id:           "id2",
			DesiredState: api.TaskState_RUNNING,
			Spec: &api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{"node.role == worker"},
				},
			},
			Status: &api.TaskStatus{
				State: api.TaskState_RUNNING,
			},
			NodeId: "id1",
		},
		{
			Id:           "id3",
			DesiredState: api.TaskState_NEW,
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			NodeId: "id2",
		},
		{
			Id:           "id4",
			DesiredState: api.TaskState_READY,
			Spec: &api.TaskSpec{
				Resources: &api.ResourceRequirements{
					Reservations: &api.Resources{
						MemoryBytes: 9e8,
					},
				},
			},
			Status: &api.TaskStatus{
				State: api.TaskState_PENDING,
			},
			NodeId: "id2",
		},
	}

	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	err := s.Update(func(tx store.Tx) error {
		// Prepoulate nodes
		for _, n := range nodes {
			assert.NoError(t, store.CreateNode(tx, n))
		}

		// Prepopulate tasks
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()

	constraintEnforcer := New(s)
	defer constraintEnforcer.Stop()

	go constraintEnforcer.Run()

	// id0 should be rejected immediately
	shutdown1 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, "id0", shutdown1.Id)
	assert.Equal(t, api.TaskState_REJECTED, shutdown1.Status.GetState())

	// Change node id1 to a manager
	err = s.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, "id1")
		if node == nil {
			t.Fatal("could not get node id1")
		}
		node.Role = api.NodeRole_MANAGER
		assert.NoError(t, store.UpdateNode(tx, node))
		return nil
	})
	assert.NoError(t, err)

	// since we've changed the node from a worker to a manager, this task
	// should now shut down
	shutdown2 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, "id2", shutdown2.Id)
	assert.Equal(t, api.TaskState_REJECTED, shutdown2.Status.GetState())

	// Change resources on node id2
	err = s.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, "id2")
		if node == nil {
			t.Fatal("could not get node id2")
		}
		node.Description.Resources.MemoryBytes = 5e8
		assert.NoError(t, store.UpdateNode(tx, node))
		return nil
	})
	assert.NoError(t, err)

	shutdown3 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, "id4", shutdown3.Id)
	assert.Equal(t, api.TaskState_REJECTED, shutdown3.Status.GetState())
}

// TestOutdatedPlacementConstraints tests the following scenario: If a task is
// associacted with a service then we must use the constraints from the current
// service spec rather than the constraints from the task spec because they may
// be outdated. This will happen if the service was previously updated in a way
// which only changes the placement constraints and the node matched the
// placement constraints both before and after that update. In the case of such
// updates, the tasks are not considered "dirty" and are not restarted but it
// will mean that the task spec's placement constraints are outdated. Consider
// this example:
//   - A service is created with no constraints and a task is scheduled
//     to a node.
//   - The node is updated to add a label, this doesn't affect the task
//     on that node because it has no constraints.
//   - The service is updated to add a node label constraint which
//     matches the label which was just added to the node. The updater
//     does not shut down the task because the only the constraints have
//     changed and the node still matches the updated constraints.
//
// This test initializes a new in-memory store with the expected state from
// above, starts a new constraint enforcer, and then updates the node to remove
// the node label. Since the node no longer satisfies the placement constraints
// of the service spec, the task should be shutdown despite the fact that the
// task's own spec still has the original placement constraints.
func TestOutdatedTaskPlacementConstraints(t *testing.T) {
	node := &api.Node{
		Id: "id0",
		Spec: &api.NodeSpec{
			Annotations: &api.Annotations{
				Name: "node1",
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			Availability: api.NodeSpec_ACTIVE,
		},
		Status: &api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRole_WORKER,
	}

	service := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "service1",
			},
			Task: &api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{
						"node.labels.foo == bar",
					},
				},
			},
		},
	}

	task := &api.Task{
		Id: "id2",
		Spec: &api.TaskSpec{
			Placement: nil, // Note: No placement constraints.
		},
		ServiceId: service.Id,
		NodeId:    node.Id,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		DesiredState: api.TaskState_RUNNING,
	}

	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	require.NoError(t, s.Update(func(tx store.Tx) error {
		// Prepopulate node, service, and task.
		for _, err := range []error{
			store.CreateNode(tx, node),
			store.CreateService(tx, service),
			store.CreateTask(tx, task),
		} {
			if err != nil {
				return err
			}
		}
		return nil
	}))

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()

	constraintEnforcer := New(s)
	defer constraintEnforcer.Stop()

	go constraintEnforcer.Run()

	// Update the node to remove the node label.
	require.NoError(t, s.Update(func(tx store.Tx) error {
		node = store.GetNode(tx, node.Id)
		delete(node.GetSpec().GetAnnotations().GetLabels(), "foo")
		return store.UpdateNode(tx, node)
	}))

	// The task should be rejected immediately.
	task = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REJECTED, task.Status.GetState())
}

func TestGenericResourcesPlacementConstraints(t *testing.T) {
	node := &api.Node{
		Id: "id0",
		Spec: &api.NodeSpec{
			Annotations: &api.Annotations{
				Name: "node1",
			},
			Availability: api.NodeSpec_ACTIVE,
		},
		Status: &api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRole_WORKER,
		Description: &api.NodeDescription{
			Resources: &api.Resources{
				Generic: genericresource.NewSet("mygeneric", "1"),
			},
		},
	}

	service := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "service1",
			},
			Task: &api.TaskSpec{
				Resources: &api.ResourceRequirements{
					Reservations: &api.Resources{
						Generic: genericresource.NewSet("mygeneric", "1"),
					},
				},
			},
		},
	}

	task := &api.Task{
		Id: "id2",
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{
				Reservations: &api.Resources{
					Generic: genericresource.NewSet("mygeneric", "1"),
				},
			},
		},
		ServiceId: service.Id,
		NodeId:    node.Id,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		DesiredState:             api.TaskState_RUNNING,
		AssignedGenericResources: genericresource.NewSet("mygeneric", "1"),
	}

	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	require.NoError(t, s.Update(func(tx store.Tx) error {
		// Prepopulate node, service, and task.
		for _, err := range []error{
			store.CreateNode(tx, node),
			store.CreateService(tx, service),
			store.CreateTask(tx, task),
		} {
			if err != nil {
				return err
			}
		}
		return nil
	}))

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()

	constraintEnforcer := New(s)
	defer constraintEnforcer.Stop()

	go constraintEnforcer.Run()

	// Update the node to remove the generic resource
	require.NoError(t, s.Update(func(tx store.Tx) error {
		node = store.GetNode(tx, node.Id)
		node.Description = &api.NodeDescription{
			Resources: &api.Resources{
				Generic: genericresource.NewSet("mygeneric", "2"),
			},
		}
		return store.UpdateNode(tx, node)
	}))

	// The task should be rejected immediately.
	task = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REJECTED, task.Status.GetState())
}

func TestGenericResourcesPlacementConstraintsDiscrete(t *testing.T) {
	node := &api.Node{
		Id: "id0",
		Spec: &api.NodeSpec{
			Annotations: &api.Annotations{
				Name: "node1",
			},
			Availability: api.NodeSpec_ACTIVE,
		},
		Status: &api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRole_WORKER,
		Description: &api.NodeDescription{
			Resources: &api.Resources{
				Generic: []*api.GenericResource{
					genericresource.NewDiscrete("mygeneric", 2),
				},
			},
		},
		Attachments: []*api.NetworkAttachment{
			{
				Network: &api.Network{
					Id: "id1",
				},
			},
		},
	}

	service := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "service1",
			},
			Task: &api.TaskSpec{
				Resources: &api.ResourceRequirements{
					Reservations: &api.Resources{
						Generic: []*api.GenericResource{
							genericresource.NewDiscrete("mygeneric", 2),
						},
					},
				},
				Networks: []*api.NetworkAttachmentConfig{
					{
						Target: "id1",
					},
				},
			},
		},
	}

	task := &api.Task{
		Id: "id2",
		Spec: &api.TaskSpec{
			Resources: &api.ResourceRequirements{
				Reservations: &api.Resources{
					Generic: []*api.GenericResource{
						genericresource.NewDiscrete("mygeneric", 2),
					},
				},
			},
			Networks: []*api.NetworkAttachmentConfig{
				{
					Target: "id1",
				},
			},
		},
		ServiceId: service.Id,
		NodeId:    node.Id,
		Status: &api.TaskStatus{
			State: api.TaskState_RUNNING,
		},
		DesiredState: api.TaskState_RUNNING,
		AssignedGenericResources: []*api.GenericResource{
			genericresource.NewDiscrete("mygeneric", 2),
		},
	}

	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	require.NoError(t, s.Update(func(tx store.Tx) error {
		// Prepopulate node, service, and task.
		for _, err := range []error{
			store.CreateNode(tx, node),
			store.CreateService(tx, service),
			store.CreateTask(tx, task),
		} {
			if err != nil {
				return err
			}
		}
		return nil
	}))

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()

	constraintEnforcer := New(s)
	defer constraintEnforcer.Stop()

	// Update the node to remove the generic resource
	require.NoError(t, s.Update(func(tx store.Tx) error {
		node = store.GetNode(tx, node.Id)
		node.Description = &api.NodeDescription{
			Resources: &api.Resources{
				Generic: []*api.GenericResource{
					genericresource.NewDiscrete("mygeneric", 1),
				},
				NanoCpus:    1e9,
				MemoryBytes: 1e9,
			},
		}
		return store.UpdateNode(tx, node)
	}))

	go constraintEnforcer.Run()

	// The task should be rejected immediately.
	task = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REJECTED, task.Status.GetState())
}
