package orchestrator

import (
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestDrain(t *testing.T) {
	ctx := context.Background()
	initialService := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
				Restart: &api.RestartPolicy{
					Condition: api.RestartOnNone,
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 6,
				},
			},
		},
	}
	initialNodeSet := []*api.Node{
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
				State: api.NodeStatus_DOWN,
			},
		},
		// We should NOT kick out tasks on UNKNOWN nodes.
		{
			ID: "id3",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name3",
				},
				Availability: api.NodeAvailabilityActive,
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_UNKNOWN,
			},
		},
		{
			ID: "id4",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name4",
				},
				Availability: api.NodeAvailabilityPause,
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			ID: "id5",
			Spec: api.NodeSpec{
				Annotations: api.Annotations{
					Name: "name5",
				},
				Availability: api.NodeAvailabilityDrain,
			},
			Status: api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
	}

	initialTaskSet := []*api.Task{
		// Task not assigned to any node
		{
			ID: "id0",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name0",
			},
			ServiceID: "id1",
		},
		// Tasks assigned to the nodes defined above
		{
			ID: "id1",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name1",
			},
			ServiceID: "id1",
			NodeID:    "id1",
		},
		{
			ID: "id2",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name2",
			},
			ServiceID: "id1",
			NodeID:    "id2",
		},
		{
			ID: "id3",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name3",
			},
			ServiceID: "id1",
			NodeID:    "id3",
		},
		{
			ID: "id4",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name4",
			},
			ServiceID: "id1",
			NodeID:    "id4",
		},
		{
			ID: "id5",
			Status: api.TaskStatus{
				State: api.TaskStateNew,
			},
			ServiceAnnotations: api.Annotations{
				Name: "name5",
			},
			ServiceID: "id1",
			NodeID:    "id5",
		},
	}

	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)

	err := s.Update(func(tx store.Tx) error {
		// Prepopulate service
		assert.NoError(t, store.CreateService(tx, initialService))
		// Prepoulate nodes
		for _, n := range initialNodeSet {
			assert.NoError(t, store.CreateNode(tx, n))
		}

		// Prepopulate tasks
		for _, task := range initialTaskSet {
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// id2 and id5 should be killed immediately
	deletion1 := watchShutdownTask(t, watch)
	deletion2 := watchShutdownTask(t, watch)

	assert.Regexp(t, "id(2|5)", deletion1.ID)
	assert.Regexp(t, "id(2|5)", deletion1.NodeID)
	assert.Regexp(t, "id(2|5)", deletion2.ID)
	assert.Regexp(t, "id(2|5)", deletion2.NodeID)

	// Create a new task, assigned to node id2
	err = s.Update(func(tx store.Tx) error {
		task := initialTaskSet[2].Copy()
		task.ID = "newtask"
		task.NodeID = "id2"
		assert.NoError(t, store.CreateTask(tx, task))
		return nil
	})
	assert.NoError(t, err)

	deletion3 := watchShutdownTask(t, watch)
	assert.Equal(t, "newtask", deletion3.ID)
	assert.Equal(t, "id2", deletion3.NodeID)

	// Set node id4 to the DRAINED state
	err = s.Update(func(tx store.Tx) error {
		n := initialNodeSet[3].Copy()
		n.Spec.Availability = api.NodeAvailabilityDrain
		assert.NoError(t, store.UpdateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	deletion4 := watchShutdownTask(t, watch)
	assert.Equal(t, "id4", deletion4.ID)
	assert.Equal(t, "id4", deletion4.NodeID)

	// Delete node id1
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteNode(tx, "id1"))
		return nil
	})
	assert.NoError(t, err)

	deletion5 := watchShutdownTask(t, watch)
	assert.Equal(t, "id1", deletion5.ID)
	assert.Equal(t, "id1", deletion5.NodeID)
}
