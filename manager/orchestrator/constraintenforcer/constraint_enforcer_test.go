package constraintenforcer

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConstraintEnforcer(t *testing.T) {
	nodes := []*api.Node{
		// this node starts as a worker, but then is changed to a manager.
		{
			ID: "id1",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name1",
				},
				Availability: api.NodeAvailabilityActive,
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Role: api.NodeRoleWorker,
		},
		{
			ID: "id2",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name2",
				},
				Availability: api.NodeAvailabilityActive,
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
			Description: &api.NodeDescription{
				Resources: &api.Resources{
					NanoCPUs:    1e9,
					MemoryBytes: 1e9,
				},
			},
		},
	}

	tasks := []*api.Task{
		// This task should not run, because id1 is a worker
		{
			ID:           "id0",
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{"node.role == manager"},
				},
			},
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			NodeID: "id1",
		},
		// this task should run without question
		{
			ID:           "id1",
			DesiredState: api.TaskStateRunning,
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			NodeID: "id1",
		},
		// this task, which might belong to a job, should run.
		{
			ID:           "id5",
			DesiredState: api.TaskStateCompleted,
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			NodeID: "id1",
		},
		// this task should run fine and not shut down at first, because node
		// id1 is correctly a worker. but when the node is updated to be a
		// manager, it should be rejected
		{
			ID:           "id2",
			DesiredState: api.TaskStateRunning,
			Spec: api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{"node.role == worker"},
				},
			},
			Status: api.TaskStatus{
				State: api.TaskStateRunning,
			},
			NodeID: "id1",
		},
		{
			ID:           "id3",
			DesiredState: api.TaskStateNew,
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			NodeID: "id2",
		},
		{
			ID:           "id4",
			DesiredState: api.TaskStateReady,
			Spec: api.TaskSpec{
				Resources: &api.ResourceRequirements{
					Reservations: &api.Resources{
						MemoryBytes: 9e8,
					},
				},
			},
			Status: api.TaskStatus{
				State: api.TaskStatePending,
			},
			NodeID: "id2",
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
	assert.Equal(t, "id0", shutdown1.ID)
	assert.Equal(t, api.TaskStateRejected, shutdown1.Status.State)

	// Change node id1 to a manager
	err = s.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, "id1")
		if node == nil {
			t.Fatal("could not get node id1")
		}
		node.Role = api.NodeRoleManager
		assert.NoError(t, store.UpdateNode(tx, node))
		return nil
	})
	assert.NoError(t, err)

	// since we've changed the node from a worker to a manager, this task
	// should now shut down
	shutdown2 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, "id2", shutdown2.ID)
	assert.Equal(t, api.TaskStateRejected, shutdown2.Status.State)

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
	assert.Equal(t, "id4", shutdown3.ID)
	assert.Equal(t, api.TaskStateRejected, shutdown3.Status.State)
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
		ID: "id0",
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: "node1",
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			Availability: api.NodeAvailabilityActive,
		},
		Status: api.NodeStatus{
			State: api.NodeStatus_READY,
		},
		Role: api.NodeRoleWorker,
	}

	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "service1",
			},
			Task: api.TaskSpec{
				Placement: &api.Placement{
					Constraints: []string{
						"node.labels.foo == bar",
					},
				},
			},
		},
	}

	task := &api.Task{
		ID: "id2",
		Spec: api.TaskSpec{
			Placement: nil, // Note: No placement constraints.
		},
		ServiceID: service.ID,
		NodeID:    node.ID,
		Status: api.TaskStatus{
			State: api.TaskStateRunning,
		},
		DesiredState: api.TaskStateRunning,
	}

	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	require.NoError(t, s.Update(func(tx store.Tx) error {
		// Prepoulate node, service, and task.
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
		node = store.GetNode(tx, node.ID)
		delete(node.Spec.Annotations.Labels, "foo")
		return store.UpdateNode(tx, node)
	}))

	// The task should be rejected immediately.
	task = testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskStateRejected, task.Status.State)
}
