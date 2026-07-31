package replicated

import (
	"context"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	durationpb "google.golang.org/protobuf/types/known/durationpb"
)

func TestReplicatedOrchestrator(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue() /*api.EventCreateTask{}, api.EventUpdateTask{}*/)
	defer cancel()

	// Create a service with two instances specified before the orchestrator is
	// started. This should result in two tasks when the orchestrator
	// starts up.
	err := s.Update(func(tx store.Tx) error {
		s1 := &api.Service{
			Id: "id1",
			Spec: &api.ServiceSpec{
				Annotations: &api.Annotations{
					Name: "name1",
				},
				Task: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 2,
					},
				},
			},
		}
		assert.NoError(t, store.CreateService(tx, s1))
		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	observedTask1 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask1.GetServiceAnnotations().GetName(), "name1")

	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.GetServiceAnnotations().GetName(), "name1")

	// Create a second service.
	err = s.Update(func(tx store.Tx) error {
		s2 := &api.Service{
			Id: "id2",
			Spec: &api.ServiceSpec{
				Annotations: &api.Annotations{
					Name: "name2",
				},
				Task: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 1,
					},
				},
			},
		}
		assert.NoError(t, store.CreateService(tx, s2))
		return nil
	})
	assert.NoError(t, err)

	observedTask3 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask3.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask3.GetServiceAnnotations().GetName(), "name2")

	// Update a service to scale it out to 3 instances
	err = s.Update(func(tx store.Tx) error {
		s2 := &api.Service{
			Id: "id2",
			Spec: &api.ServiceSpec{
				Annotations: &api.Annotations{
					Name: "name2",
				},
				Task: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 3,
					},
				},
			},
		}
		assert.NoError(t, store.UpdateService(tx, s2))
		return nil
	})
	assert.NoError(t, err)

	observedTask4 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask4.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask4.GetServiceAnnotations().GetName(), "name2")

	observedTask5 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask5.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask5.GetServiceAnnotations().GetName(), "name2")

	// Now scale it back down to 1 instance
	err = s.Update(func(tx store.Tx) error {
		s2 := &api.Service{
			Id: "id2",
			Spec: &api.ServiceSpec{
				Annotations: &api.Annotations{
					Name: "name2",
				},
				Task: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				Mode: &api.ServiceSpec_Replicated{
					Replicated: &api.ReplicatedService{
						Replicas: 1,
					},
				},
			},
		}
		assert.NoError(t, store.UpdateService(tx, s2))
		return nil
	})
	assert.NoError(t, err)

	observedUpdateRemove1 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedUpdateRemove1.DesiredState, api.TaskState_REMOVE)
	assert.Equal(t, observedUpdateRemove1.GetServiceAnnotations().GetName(), "name2")

	observedUpdateRemove2 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedUpdateRemove2.DesiredState, api.TaskState_REMOVE)
	assert.Equal(t, observedUpdateRemove2.GetServiceAnnotations().GetName(), "name2")

	// There should be one remaining task attached to service id2/name2.
	var liveTasks []*api.Task
	s.View(func(readTx store.ReadTx) {
		var tasks []*api.Task
		tasks, err = store.FindTasks(readTx, store.ByServiceID("id2"))
		for _, t := range tasks {
			if t.DesiredState == api.TaskState_RUNNING {
				liveTasks = append(liveTasks, t)
			}
		}
	})
	assert.NoError(t, err)
	assert.Len(t, liveTasks, 1)

	// Delete the remaining task directly. It should be recreated by the
	// orchestrator.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteTask(tx, liveTasks[0].Id))
		return nil
	})
	assert.NoError(t, err)

	observedTask6 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask6.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask6.GetServiceAnnotations().GetName(), "name2")

	// Delete the service. Its remaining task should go away.
	err = s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.DeleteService(tx, "id2"))
		return nil
	})
	assert.NoError(t, err)

	deletedTask := testutils.WatchTaskDelete(t, watch)
	assert.Equal(t, deletedTask.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, deletedTask.GetServiceAnnotations().GetName(), "name2")
}

func TestReplicatedScaleDown(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{}, api.EventDeleteTask{})
	defer cancel()

	s1 := &api.Service{
		Id: "id1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 6,
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, s1))

		nodes := []*api.Node{
			{
				Id: "node1",
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
				Id: "node2",
				Spec: &api.NodeSpec{
					Annotations: &api.Annotations{
						Name: "name2",
					},
					Availability: api.NodeSpec_ACTIVE,
				},
				Status: &api.NodeStatus{
					State: api.NodeStatus_READY,
				},
			},
			{
				Id: "node3",
				Spec: &api.NodeSpec{
					Annotations: &api.Annotations{
						Name: "name3",
					},
					Availability: api.NodeSpec_ACTIVE,
				},
				Status: &api.NodeStatus{
					State: api.NodeStatus_READY,
				},
			},
		}
		for _, node := range nodes {
			assert.NoError(t, store.CreateNode(tx, node))
		}

		// task1 is assigned to node1
		// task2 - task3 are assigned to node2
		// task4 - task6 are assigned to node3
		// task7 is unassigned

		tasks := []*api.Task{
			{
				Id:           "task1",
				Slot:         1,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_STARTING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task1",
				},
				ServiceId: "id1",
				NodeId:    "node1",
			},
			{
				Id:           "task2",
				Slot:         2,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task2",
				},
				ServiceId: "id1",
				NodeId:    "node2",
			},
			{
				Id:           "task3",
				Slot:         3,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task3",
				},
				ServiceId: "id1",
				NodeId:    "node2",
			},
			{
				Id:           "task4",
				Slot:         4,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task4",
				},
				ServiceId: "id1",
				NodeId:    "node3",
			},
			{
				Id:           "task5",
				Slot:         5,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task5",
				},
				ServiceId: "id1",
				NodeId:    "node3",
			},
			{
				Id:           "task6",
				Slot:         6,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task6",
				},
				ServiceId: "id1",
				NodeId:    "node3",
			},
			{
				Id:           "task7",
				Slot:         7,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_NEW,
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task7",
				},
				ServiceId: "id1",
			},
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}

		return nil
	})
	assert.NoError(t, err)

	// Start the orchestrator.
	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// Replicas was set to 6, but we started with 7 tasks. task7 should
	// be the one the orchestrator chose to shut down because it was not
	// assigned yet. The desired state of task7 will be set to "REMOVE"

	observedUpdateRemove := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, api.TaskState_REMOVE, observedUpdateRemove.DesiredState)
	assert.Equal(t, "task7", observedUpdateRemove.Id)

	// Now scale down to 4 instances.
	err = s.Update(func(tx store.Tx) error {
		s1.Spec.Mode = &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: 4,
			},
		}
		assert.NoError(t, store.UpdateService(tx, s1))
		return nil
	})
	assert.NoError(t, err)

	// Tasks should be shut down in a way that balances the remaining tasks.
	// node2 should be preferred over node3 because node2's tasks have
	// lower Slot numbers than node3's tasks.

	shutdowns := make(map[string]int)
	for range 2 {
		observedUpdateDesiredRemove := testutils.WatchTaskUpdate(t, watch)
		assert.Equal(t, api.TaskState_REMOVE, observedUpdateDesiredRemove.DesiredState)
		shutdowns[observedUpdateDesiredRemove.NodeId]++
	}

	assert.Equal(t, 0, shutdowns["node1"])
	assert.Equal(t, 0, shutdowns["node2"])
	assert.Equal(t, 2, shutdowns["node3"])

	// task4 should be preferred over task5 and task6.
	s.View(func(readTx store.ReadTx) {
		tasks, err := store.FindTasks(readTx, store.ByNodeID("node3"))
		require.NoError(t, err)
		for _, task := range tasks {
			if task.DesiredState == api.TaskState_RUNNING {
				assert.Equal(t, "task4", task.Id)
			}
		}
	})

	// Now scale down to 2 instances.
	err = s.Update(func(tx store.Tx) error {
		s1.Spec.Mode = &api.ServiceSpec_Replicated{
			Replicated: &api.ReplicatedService{
				Replicas: 2,
			},
		}
		assert.NoError(t, store.UpdateService(tx, s1))
		return nil
	})
	assert.NoError(t, err)

	// Tasks should be shut down in a way that balances the remaining tasks.
	// node2 and node3 should be preferred over node1 because node1's task
	// is not running yet.

	shutdowns = make(map[string]int)
	for range 2 {
		observedUpdateDesiredRemove := testutils.WatchTaskUpdate(t, watch)
		assert.Equal(t, api.TaskState_REMOVE, observedUpdateDesiredRemove.DesiredState)
		shutdowns[observedUpdateDesiredRemove.NodeId]++
	}

	assert.Equal(t, 1, shutdowns["node1"])
	assert.Equal(t, 1, shutdowns["node2"])
	assert.Equal(t, 0, shutdowns["node3"])

	// There should be remaining tasks on node2 and node3. task2 should be
	// preferred over task3 on node2.
	s.View(func(readTx store.ReadTx) {
		tasks, err := store.FindTasks(readTx, store.ByDesiredState(api.TaskState_RUNNING))
		require.NoError(t, err)
		require.Len(t, tasks, 2)
		if tasks[0].NodeId == "node2" {
			assert.Equal(t, "task2", tasks[0].Id)
			assert.Equal(t, "node3", tasks[1].NodeId)
		} else {
			assert.Equal(t, "node3", tasks[0].NodeId)
			assert.Equal(t, "node2", tasks[1].NodeId)
			assert.Equal(t, "task2", tasks[1].Id)
		}
	})
}

func TestInitializationRejectedTasks(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	service1 := &api.Service{
		Id: "serviceid1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))

		nodes := []*api.Node{
			{
				Id: "node1",
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
		}
		for _, node := range nodes {
			assert.NoError(t, store.CreateNode(tx, node))
		}

		// 1 rejected task is in store before orchestrator starts
		tasks := []*api.Task{
			{
				Id:           "task1",
				Slot:         1,
				DesiredState: api.TaskState_READY,
				Status: &api.TaskStatus{
					State: api.TaskState_REJECTED,
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task1",
				},
				ServiceId: "serviceid1",
				NodeId:    "node1",
			},
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}

		return nil
	})
	assert.NoError(t, err)

	// watch orchestration events
	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{}, api.EventDeleteTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// initTask triggers an update event
	observedTask1 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask1.Id, "task1")
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_REJECTED)
	assert.Equal(t, observedTask1.DesiredState, api.TaskState_SHUTDOWN)

	// a new task is created
	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.ServiceId, "serviceid1")
	// it has not been scheduled
	assert.Equal(t, observedTask2.NodeId, "")
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.DesiredState, api.TaskState_READY)

	var deadCnt, liveCnt int
	s.View(func(readTx store.ReadTx) {
		var tasks []*api.Task
		tasks, err = store.FindTasks(readTx, store.ByServiceID("serviceid1"))
		for _, task := range tasks {
			if task.DesiredState == api.TaskState_SHUTDOWN {
				assert.Equal(t, task.Id, "task1")
				deadCnt++
			} else {
				liveCnt++
			}
		}
	})
	assert.NoError(t, err)
	assert.Equal(t, deadCnt, 1)
	assert.Equal(t, liveCnt, 1)
}

func TestInitializationFailedTasks(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	service1 := &api.Service{
		Id: "serviceid1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 2,
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))

		nodes := []*api.Node{
			{
				Id: "node1",
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
		}
		for _, node := range nodes {
			assert.NoError(t, store.CreateNode(tx, node))
		}

		// 1 failed task is in store before orchestrator starts
		tasks := []*api.Task{
			{
				Id:           "task1",
				Slot:         1,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_FAILED,
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task1",
				},
				ServiceId: "serviceid1",
				NodeId:    "node1",
			},
			{
				Id:           "task2",
				Slot:         2,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_STARTING,
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task2",
				},
				ServiceId: "serviceid1",
				NodeId:    "node1",
			},
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}

		return nil
	})
	assert.NoError(t, err)

	// watch orchestration events
	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{}, api.EventDeleteTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// initTask triggers an update
	observedTask1 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask1.Id, "task1")
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_FAILED)
	assert.Equal(t, observedTask1.DesiredState, api.TaskState_SHUTDOWN)

	// a new task is created
	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.ServiceId, "serviceid1")
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.DesiredState, api.TaskState_READY)

	var deadCnt, liveCnt int
	s.View(func(readTx store.ReadTx) {
		var tasks []*api.Task
		tasks, err = store.FindTasks(readTx, store.ByServiceID("serviceid1"))
		for _, task := range tasks {
			if task.DesiredState == api.TaskState_SHUTDOWN {
				assert.Equal(t, task.Id, "task1")
				deadCnt++
			} else {
				liveCnt++
			}
		}
	})
	assert.NoError(t, err)
	assert.Equal(t, deadCnt, 1)
	assert.Equal(t, liveCnt, 2)
}

func TestInitializationNodeDown(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	service1 := &api.Service{
		Id: "serviceid1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))

		nodes := []*api.Node{
			{
				Id: "node1",
				Spec: &api.NodeSpec{
					Annotations: &api.Annotations{
						Name: "name1",
					},
					Availability: api.NodeSpec_ACTIVE,
				},
				Status: &api.NodeStatus{
					State: api.NodeStatus_DOWN,
				},
			},
		}
		for _, node := range nodes {
			assert.NoError(t, store.CreateNode(tx, node))
		}

		// 1 failed task is in store before orchestrator starts
		tasks := []*api.Task{
			{
				Id:           "task1",
				Slot:         1,
				DesiredState: api.TaskState_RUNNING,
				Status: &api.TaskStatus{
					State: api.TaskState_RUNNING,
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task1",
				},
				ServiceId: "serviceid1",
				NodeId:    "node1",
			},
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}

		return nil
	})
	assert.NoError(t, err)

	// watch orchestration events
	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{}, api.EventDeleteTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// initTask triggers an update
	observedTask1 := testutils.WatchTaskUpdate(t, watch)
	assert.Equal(t, observedTask1.Id, "task1")
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_RUNNING)
	assert.Equal(t, observedTask1.DesiredState, api.TaskState_SHUTDOWN)

	// a new task is created
	observedTask2 := testutils.WatchTaskCreate(t, watch)
	assert.Equal(t, observedTask2.ServiceId, "serviceid1")
	assert.Equal(t, observedTask2.Status.GetState(), api.TaskState_NEW)
	assert.Equal(t, observedTask2.DesiredState, api.TaskState_READY)
}

func TestInitializationDelayStart(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	service1 := &api.Service{
		Id: "serviceid1",
		Spec: &api.ServiceSpec{
			Annotations: &api.Annotations{
				Name: "name1",
			},
			Task: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{},
				},
				Restart: &api.RestartPolicy{
					Condition: api.RestartPolicy_ANY,
					Delay:     durationpb.New(100 * time.Millisecond),
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: 1,
				},
			},
		},
	}

	before := time.Now()
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service1))

		nodes := []*api.Node{
			{
				Id: "node1",
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
		}
		for _, node := range nodes {
			assert.NoError(t, store.CreateNode(tx, node))
		}

		// 1 failed task is in store before orchestrator starts
		tasks := []*api.Task{
			{
				Id:           "task1",
				Slot:         1,
				DesiredState: api.TaskState_READY,
				Status: &api.TaskStatus{
					State:     api.TaskState_READY,
					Timestamp: ptypes.MustTimestampProto(before),
				},
				Spec: &api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{},
					},
					Restart: &api.RestartPolicy{
						Condition: api.RestartPolicy_ANY,
						Delay:     durationpb.New(100 * time.Millisecond),
					},
				},
				ServiceAnnotations: &api.Annotations{
					Name: "task1",
				},
				ServiceId: "serviceid1",
				NodeId:    "node1",
			},
		}
		for _, task := range tasks {
			assert.NoError(t, store.CreateTask(tx, task))
		}

		return nil
	})
	assert.NoError(t, err)

	// watch orchestration events
	watch, cancel := state.Watch(s.WatchQueue(), api.EventCreateTask{}, api.EventUpdateTask{}, api.EventDeleteTask{})
	defer cancel()

	orchestrator := NewReplicatedOrchestrator(s)
	defer orchestrator.Stop()

	go func() {
		assert.NoError(t, orchestrator.Run(ctx))
	}()

	// initTask triggers an update
	observedTask1 := testutils.WatchTaskUpdate(t, watch)
	after := time.Now()
	assert.Equal(t, observedTask1.Id, "task1")
	assert.Equal(t, observedTask1.Status.GetState(), api.TaskState_READY)
	assert.Equal(t, observedTask1.DesiredState, api.TaskState_RUNNING)

	// At least 100 ms should have elapsed
	if after.Sub(before) < 100*time.Millisecond {
		t.Fatalf("restart delay should have elapsed. Got: %v", after.Sub(before))
	}
}
