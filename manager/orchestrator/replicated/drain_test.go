package replicated

import (
	"context"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func TestDrain(t *testing.T) {
	ctx := context.Background()
	initialService := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
				Restart: &api.RestartPolicy{
					Condition: api.RestartPolicy_NONE,
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
				State: api.NodeStatus_DOWN,
			},
		},
		// We should NOT kick out tasks on UNKNOWN nodes.
		{
			Id: "id3",
			Spec: &api.NodeSpec{
				Annotations: &api.Annotations{
					Name: "name3",
				},
				Availability: api.NodeSpec_ACTIVE,
			},
			Status: &api.NodeStatus{
				State: api.NodeStatus_UNKNOWN,
			},
		},
		{
			Id: "id4",
			Spec: &api.NodeSpec{
				Annotations: &api.Annotations{
					Name: "name4",
				},
				Availability: api.NodeSpec_PAUSE,
			},
			Status: &api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
		{
			Id: "id5",
			Spec: &api.NodeSpec{
				Annotations: &api.Annotations{
					Name: "name5",
				},
				Availability: api.NodeSpec_DRAIN,
			},
			Status: &api.NodeStatus{
				State: api.NodeStatus_READY,
			},
		},
	}

	initialTaskSet := []*api.Task{
		// Task not assigned to any node
		{
			Id:           "id0",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 1,
			ServiceAnnotations: &api.Annotations{
				Name: "name0",
			},
			ServiceId: "id1",
		},
		// Tasks assigned to the nodes defined above
		{
			Id:           "id1",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 2,
			ServiceAnnotations: &api.Annotations{
				Name: "name1",
			},
			ServiceId: "id1",
			NodeId:    "id1",
		},
		{
			Id:           "id2",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 3,
			ServiceAnnotations: &api.Annotations{
				Name: "name2",
			},
			ServiceId: "id1",
			NodeId:    "id2",
		},
		{
			Id:           "id3",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 4,
			ServiceAnnotations: &api.Annotations{
				Name: "name3",
			},
			ServiceId: "id1",
			NodeId:    "id3",
		},
		{
			Id:           "id4",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 5,
			ServiceAnnotations: &api.Annotations{
				Name: "name4",
			},
			ServiceId: "id1",
			NodeId:    "id4",
		},
		{
			Id:           "id5",
			DesiredState: api.TaskState_RUNNING,
			Spec:         initialService.Spec.GetTask(),
			Status: &api.TaskStatus{
				State: api.TaskState_NEW,
			},
			Slot: 6,
			ServiceAnnotations: &api.Annotations{
				Name: "name5",
			},
			ServiceId: "id1",
			NodeId:    "id5",
		},
	}

	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

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

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// id2 and id5 should be killed immediately
	deletion1 := testutils.WatchShutdownTask(t, watch)
	deletion2 := testutils.WatchShutdownTask(t, watch)

	assert.Regexp(t, "id(2|5)", deletion1.Id)
	assert.Regexp(t, "id(2|5)", deletion1.NodeId)
	assert.Regexp(t, "id(2|5)", deletion2.Id)
	assert.Regexp(t, "id(2|5)", deletion2.NodeId)

	// Create a new task, assigned to node id2
	err = s.Update(func(tx store.Tx) error {
		task := initialTaskSet[2].Copy()
		task.Id = "newtask"
		task.NodeId = "id2"
		assert.NoError(t, store.CreateTask(tx, task))
		return nil
	})
	assert.NoError(t, err)

	deletion3 := testutils.WatchShutdownTask(t, watch)
	assert.Equal(t, "newtask", deletion3.Id)
	assert.Equal(t, "id2", deletion3.NodeId)

	// Set node id4 to the DRAINED state
	err = s.Update(func(tx store.Tx) error {
		n := initialNodeSet[3].Copy()
		n.Spec.Availability = api.NodeSpec_DRAIN
		assert.NoError(t, store.UpdateNode(tx, n))
		return nil
	})
	assert.NoError(t, err)

	deletion4 := testutils.WatchShutdownTask(t, watch)
	assert.Equal(t, "id4", deletion4.Id)
	assert.Equal(t, "id4", deletion4.NodeId)

	// Delete node id1
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteNode(tx, "id1"))
		return nil
	})
	assert.NoError(t, err)

	deletion5 := testutils.WatchShutdownTask(t, watch)
	assert.Equal(t, "id1", deletion5.Id)
	assert.Equal(t, "id1", deletion5.NodeId)
}
