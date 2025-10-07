package update

import (
	"context"
	"testing"
	"time"

	gogotypes "github.com/gogo/protobuf/types"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator"
	"github.com/moby/swarmkit/v2/manager/orchestrator/restart"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func getRunnableSlotSlice(t *testing.T, s *store.MemoryStore, service *api.Service) []orchestrator.Slot {
	var (
		tasks []*api.Task
		err   error
	)
	s.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	require.NoError(t, err)

	runningSlots := make(map[uint64]orchestrator.Slot)
	for _, t := range tasks {
		if t.DesiredState <= api.TaskStateRunning {
			runningSlots[t.Slot] = append(runningSlots[t.Slot], t)
		}
	}

	var runnableSlice []orchestrator.Slot
	for _, slot := range runningSlots {
		runnableSlice = append(runnableSlice, slot)
	}
	return runnableSlice
}

func getRunningServiceTasks(t *testing.T, s *store.MemoryStore, service *api.Service) []*api.Task {
	var (
		err   error
		tasks []*api.Task
	)

	s.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	assert.NoError(t, err)

	running := []*api.Task{}
	for _, task := range tasks {
		if task.Status.State == api.TaskStateRunning {
			running = append(running, task)
		}
	}
	return running
}

func TestUpdater(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			if task.Status.State == task.DesiredState {
				continue
			}
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				task.Status.State = task.DesiredState
				return store.UpdateTask(tx, task)
			})
			assert.NoError(t, err)
		}
	}()

	instances := 3
	cluster := &api.Cluster{
		// test cluster configuration propagation to task creation.
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
		},
	}

	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: uint64(instances),
				},
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
			Update: &api.UpdateConfig{
				// avoid having Run block for a long time to watch for failures
				Monitor: gogotypes.DurationProto(50 * time.Millisecond),
			},
		},
	}

	// Create the cluster, service, and tasks for the service.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateCluster(tx, cluster))
		assert.NoError(t, store.CreateService(tx, service))
		for i := 0; i < instances; i++ {
			assert.NoError(t, store.CreateTask(tx, orchestrator.NewTask(cluster, service, uint64(i), "")))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableSlotSlice(t, s, service)
	for _, slot := range originalTasks {
		for _, task := range slot {
			assert.Equal(t, "v:1", task.Spec.GetContainer().Image)
			assert.Nil(t, task.LogDriver) // should be left alone
		}
	}

	// Change the image and log driver to force an update.
	service.Spec.Task.GetContainer().Image = "v:2"
	service.Spec.Task.LogDriver = &api.Driver{Name: "tasklogdriver"}
	updater := NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks := getRunnableSlotSlice(t, s, service)
	for _, slot := range updatedTasks {
		for _, task := range slot {
			assert.Equal(t, "v:2", task.Spec.GetContainer().Image)
			assert.Equal(t, service.Spec.Task.LogDriver, task.LogDriver) // pick up from task
		}
	}

	// Update the spec again to force an update.
	service.Spec.Task.GetContainer().Image = "v:3"
	cluster.Spec.TaskDefaults.LogDriver = &api.Driver{Name: "clusterlogdriver"} // make cluster default logdriver.
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
		Monitor:     gogotypes.DurationProto(50 * time.Millisecond),
	}
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks = getRunnableSlotSlice(t, s, service)
	for _, slot := range updatedTasks {
		for _, task := range slot {
			assert.Equal(t, "v:3", task.Spec.GetContainer().Image)
			assert.Equal(t, service.Spec.Task.LogDriver, task.LogDriver) // still pick up from task
		}
	}

	service.Spec.Task.GetContainer().Image = "v:4"
	service.Spec.Task.LogDriver = nil // use cluster default now.
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
		Delay:       10 * time.Millisecond,
		Monitor:     gogotypes.DurationProto(50 * time.Millisecond),
	}
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks = getRunnableSlotSlice(t, s, service)
	for _, slot := range updatedTasks {
		for _, task := range slot {
			assert.Equal(t, "v:4", task.Spec.GetContainer().Image)
			assert.Equal(t, cluster.Spec.TaskDefaults.LogDriver, task.LogDriver) // pick up from cluster
		}
	}

	service.Spec.Task.GetContainer().Image = "v:5"
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
		Delay:       10 * time.Millisecond,
		Order:       api.UpdateConfig_START_FIRST,
		Monitor:     gogotypes.DurationProto(50 * time.Millisecond),
	}
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks = getRunnableSlotSlice(t, s, service)
	assert.Equal(t, instances, len(updatedTasks))
	for _, instance := range updatedTasks {
		for _, task := range instance {
			assert.Equal(t, "v:5", task.Spec.GetContainer().Image)
		}
	}

	// Update pull options with new registry auth.
	service.Spec.Task.GetContainer().PullOptions = &api.ContainerSpec_PullOptions{
		RegistryAuth: "opaque-token-1",
	}
	originalTasks = getRunnableSlotSlice(t, s, service)
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, originalTasks)
	updatedTasks = getRunnableSlotSlice(t, s, service)
	assert.Len(t, updatedTasks, instances)

	// Confirm that the original runnable tasks are all still there.
	runnableTaskIDs := make(map[string]struct{}, len(updatedTasks))
	for _, slot := range updatedTasks {
		for _, task := range slot {
			runnableTaskIDs[task.ID] = struct{}{}
		}
	}
	assert.Len(t, runnableTaskIDs, instances)
	for _, slot := range originalTasks {
		for _, task := range slot {
			assert.Contains(t, runnableTaskIDs, task.ID)
		}
	}

	// Update the desired state of the tasks to SHUTDOWN to simulate the
	// case where images failed to pull due to bad registry auth.
	taskSlots := make([]orchestrator.Slot, len(updatedTasks))
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		for i, slot := range updatedTasks {
			taskSlots[i] = make(orchestrator.Slot, len(slot))
			for j, task := range slot {
				task = store.GetTask(tx, task.ID)
				task.DesiredState = api.TaskStateShutdown
				task.Status.State = task.DesiredState
				assert.NoError(t, store.UpdateTask(tx, task))
				taskSlots[i][j] = task
			}
		}
		return nil
	}))

	// Update pull options again with a different registry auth.
	service.Spec.Task.GetContainer().PullOptions = &api.ContainerSpec_PullOptions{
		RegistryAuth: "opaque-token-2",
	}
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, taskSlots) // Note that these tasks are all shutdown.
	updatedTasks = getRunnableSlotSlice(t, s, service)
	assert.Len(t, updatedTasks, instances)
}

func TestUpdaterPlacement(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			if task.Status.State == task.DesiredState {
				continue
			}
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				task.Status.State = task.DesiredState
				return store.UpdateTask(tx, task)
			})
			assert.NoError(t, err)
		}
	}()

	instances := 3
	cluster := &api.Cluster{
		// test cluster configuration propagation to task creation.
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
		},
	}

	service := &api.Service{
		ID:          "id1",
		SpecVersion: &api.Version{Index: 1},
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: uint64(instances),
				},
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
			Update: &api.UpdateConfig{
				// avoid having Run block for a long time to watch for failures
				Monitor: gogotypes.DurationProto(50 * time.Millisecond),
			},
		},
	}

	node := &api.Node{ID: "node1"}

	// Create the cluster, service, and tasks for the service.
	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateCluster(tx, cluster))
		assert.NoError(t, store.CreateService(tx, service))
		store.CreateNode(tx, node)
		for i := 0; i < instances; i++ {
			assert.NoError(t, store.CreateTask(tx, orchestrator.NewTask(cluster, service, uint64(i), "node1")))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableSlotSlice(t, s, service)
	originalTasksMaps := make([]map[string]*api.Task, len(originalTasks))
	originalTaskCount := 0
	for i, slot := range originalTasks {
		originalTasksMaps[i] = make(map[string]*api.Task)
		for _, task := range slot {
			originalTasksMaps[i][task.GetID()] = task
			assert.Equal(t, "v:1", task.Spec.GetContainer().Image)
			assert.Nil(t, task.Spec.Placement)
			originalTaskCount++
		}
	}

	// Change the placement constraints
	service.SpecVersion.Index++
	service.Spec.Task.Placement = &api.Placement{}
	service.Spec.Task.Placement.Constraints = append(service.Spec.Task.Placement.Constraints, "node.name=*")
	updater := NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks := getRunnableSlotSlice(t, s, service)
	updatedTaskCount := 0
	for _, slot := range updatedTasks {
		for _, task := range slot {
			for i, slot := range originalTasks {
				originalTasksMaps[i] = make(map[string]*api.Task)
				for _, tasko := range slot {
					if task.GetID() == tasko.GetID() {
						updatedTaskCount++
					}
				}
			}
		}
	}
	assert.Equal(t, originalTaskCount, updatedTaskCount)
}

func TestUpdaterFailureAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Fail new tasks the updater tries to run
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			if task.DesiredState == api.TaskStateRunning && task.Status.State != api.TaskStateFailed {
				err := s.Update(func(tx store.Tx) error {
					task = store.GetTask(tx, task.ID)
					task.Status.State = api.TaskStateFailed
					return store.UpdateTask(tx, task)
				})
				assert.NoError(t, err)
			} else if task.DesiredState > api.TaskStateRunning {
				err := s.Update(func(tx store.Tx) error {
					task = store.GetTask(tx, task.ID)
					task.Status.State = task.DesiredState
					return store.UpdateTask(tx, task)
				})
				assert.NoError(t, err)
			}
		}
	}()

	instances := 3
	cluster := &api.Cluster{
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
		},
	}

	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: uint64(instances),
				},
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
			Update: &api.UpdateConfig{
				FailureAction: api.UpdateConfig_PAUSE,
				Parallelism:   1,
				Delay:         500 * time.Millisecond,
				Monitor:       gogotypes.DurationProto(500 * time.Millisecond),
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateCluster(tx, cluster))
		assert.NoError(t, store.CreateService(tx, service))
		for i := 0; i < instances; i++ {
			assert.NoError(t, store.CreateTask(tx, orchestrator.NewTask(cluster, service, uint64(i), "")))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableSlotSlice(t, s, service)
	for _, slot := range originalTasks {
		for _, task := range slot {
			assert.Equal(t, "v:1", task.Spec.GetContainer().Image)
		}
	}

	service.Spec.Task.GetContainer().Image = "v:2"
	updater := NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks := getRunnableSlotSlice(t, s, service)
	v1Counter := 0
	v2Counter := 0
	for _, slot := range updatedTasks {
		for _, task := range slot {
			if task.Spec.GetContainer().Image == "v:1" {
				v1Counter++
			} else if task.Spec.GetContainer().Image == "v:2" {
				v2Counter++
			}
		}
	}
	assert.Equal(t, instances-1, v1Counter)
	assert.Equal(t, 1, v2Counter)

	s.View(func(tx store.ReadTx) {
		service = store.GetService(tx, service.ID)
	})
	assert.Equal(t, api.UpdateStatus_PAUSED, service.UpdateStatus.State)

	// Updating again should do nothing while the update is PAUSED
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks = getRunnableSlotSlice(t, s, service)
	v1Counter = 0
	v2Counter = 0
	for _, slot := range updatedTasks {
		for _, task := range slot {
			if task.Spec.GetContainer().Image == "v:1" {
				v1Counter++
			} else if task.Spec.GetContainer().Image == "v:2" {
				v2Counter++
			}
		}
	}
	assert.Equal(t, instances-1, v1Counter)
	assert.Equal(t, 1, v2Counter)

	// Switch to a service with FailureAction: CONTINUE
	err = s.Update(func(tx store.Tx) error {
		service = store.GetService(tx, service.ID)
		service.Spec.Update.FailureAction = api.UpdateConfig_CONTINUE
		service.UpdateStatus = nil
		assert.NoError(t, store.UpdateService(tx, service))
		return nil
	})
	assert.NoError(t, err)

	service.Spec.Task.GetContainer().Image = "v:3"
	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks = getRunnableSlotSlice(t, s, service)
	v2Counter = 0
	v3Counter := 0
	for _, slot := range updatedTasks {
		for _, task := range slot {
			if task.Spec.GetContainer().Image == "v:2" {
				v2Counter++
			} else if task.Spec.GetContainer().Image == "v:3" {
				v3Counter++
			}
		}
	}

	assert.Equal(t, 0, v2Counter)
	assert.Equal(t, instances, v3Counter)
}

func TestUpdaterTaskTimeout(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				// Explicitly do not set task state to
				// DEAD to trigger TaskTimeout
				if task.DesiredState == api.TaskStateRunning && task.Status.State != api.TaskStateRunning {
					task.Status.State = api.TaskStateRunning
					return store.UpdateTask(tx, task)
				}
				return nil
			})
			assert.NoError(t, err)
		}
	}()

	var instances uint64 = 3
	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: instances,
				},
			},
			Update: &api.UpdateConfig{
				// avoid having Run block for a long time to watch for failures
				Monitor: gogotypes.DurationProto(50 * time.Millisecond),
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service))
		for i := uint64(0); i < instances; i++ {
			task := orchestrator.NewTask(nil, service, uint64(i), "")
			task.Status.State = api.TaskStateRunning
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableSlotSlice(t, s, service)
	for _, slot := range originalTasks {
		for _, task := range slot {
			assert.Equal(t, "v:1", task.Spec.GetContainer().Image)
		}
	}

	before := time.Now()

	service.Spec.Task.GetContainer().Image = "v:2"
	updater := NewUpdater(s, restart.NewSupervisor(s), nil, service)
	// Override the default (1 minute) to speed up the test.
	updater.restarts.TaskTimeout = 100 * time.Millisecond
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	updatedTasks := getRunnableSlotSlice(t, s, service)
	for _, slot := range updatedTasks {
		for _, task := range slot {
			assert.Equal(t, "v:2", task.Spec.GetContainer().Image)
		}
	}

	after := time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	if after.Sub(before) < 100*time.Millisecond {
		t.Fatal("stop timeout should have elapsed")
	}
}

func TestUpdaterOrder(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			if task.Status.State == task.DesiredState {
				continue
			}
			if task.DesiredState == api.TaskStateShutdown {
				// dont progress, simulate that task takes time to shutdown
				continue
			}
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				task.Status.State = task.DesiredState
				return store.UpdateTask(tx, task)
			})
			assert.NoError(t, err)
		}
	}()

	instances := 3
	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image:           "v:1",
						StopGracePeriod: gogotypes.DurationProto(time.Hour),
					},
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: uint64(instances),
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service))
		for i := 0; i < instances; i++ {
			assert.NoError(t, store.CreateTask(tx, orchestrator.NewTask(nil, service, uint64(i), "")))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableSlotSlice(t, s, service)
	for _, instance := range originalTasks {
		for _, task := range instance {
			assert.Equal(t, "v:1", task.Spec.GetContainer().Image)
			// progress task from New to Running
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				task.Status.State = task.DesiredState
				return store.UpdateTask(tx, task)
			})
			assert.NoError(t, err)
		}
	}
	service.Spec.Task.GetContainer().Image = "v:2"
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
		Order:       api.UpdateConfig_START_FIRST,
		Delay:       10 * time.Millisecond,
		Monitor:     gogotypes.DurationProto(50 * time.Millisecond),
	}
	updater := NewUpdater(s, restart.NewSupervisor(s), nil, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))
	allTasks := getRunningServiceTasks(t, s, service)
	assert.Equal(t, instances*2, len(allTasks))
	for _, task := range allTasks {
		if task.Spec.GetContainer().Image == "v:1" {
			assert.Equal(t, task.DesiredState, api.TaskStateShutdown)
		} else if task.Spec.GetContainer().Image == "v:2" {
			assert.Equal(t, task.DesiredState, api.TaskStateRunning)
		}
	}
}

//  Tests special cases of Run() where slots contain multiple tasks.
//  These cases are handled by the useExistingTask() function.
func TestUpdaterUseExistingTask(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	require.NotNil(t, s)
	defer s.Close()

	// Setup simple watcher which moves tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), api.EventUpdateTask{})
	defer cancel()
	go func() {
		for e := range watch {
			task := e.(api.EventUpdateTask).Task
			if task.Status.State == task.DesiredState {
				continue
			}
			err := s.Update(func(tx store.Tx) error {
				task = store.GetTask(tx, task.ID)
				task.Status.State = task.DesiredState
				return store.UpdateTask(tx, task)
			})
			require.NoError(t, err)
		}
	}()

	instances := 2
	cluster := &api.Cluster{
		// test cluster configuration propagation to task creation.
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
		},
	}

	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Replicas: uint64(instances),
				},
			},
			Task: api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.ContainerSpec{
						Image: "v:1",
					},
				},
			},
			Update: &api.UpdateConfig{
				// avoid having Run block for a long time to watch for failures
				Monitor: gogotypes.DurationProto(50 * time.Millisecond),
			},
		},
	}

	// 1: First test creates two tasks for the same slot and calls updater.Run()
	// and expects one task to be running and one to be shutdown.
	var (
		firstTask  *api.Task
		secondTask *api.Task
	)
	err := s.Update(func(tx store.Tx) error {
		require.NoError(t, store.CreateCluster(tx, cluster))
		require.NoError(t, store.CreateService(tx, service))
		firstTask = orchestrator.NewTask(cluster, service, 0, "")
		require.NoError(t, store.CreateTask(tx, firstTask))
		secondTask = orchestrator.NewTask(cluster, service, 0, "")
		require.NoError(t, store.CreateTask(tx, secondTask))
		return nil
	})
	require.NoError(t, err)

	updater := NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))

	var tasks []*api.Task
	s.View(func(tx store.ReadTx) {
		tasks, _ = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	assert.True(t, (tasks[0].DesiredState == api.TaskStateRunning &&
		tasks[1].DesiredState == api.TaskStateShutdown) ||
		(tasks[1].DesiredState == api.TaskStateRunning &&
			tasks[0].DesiredState == api.TaskStateShutdown),
		"Expected one task to be desired RUNNING and the other to be desired SHUTDOWN, but they were in %v and %v",
		tasks[0].DesiredState, tasks[1].DesiredState)

	// 2: Next we set the second task to ready and expect the same results.
	err = s.Update(func(tx store.Tx) error {
		firstTask.DesiredState = api.TaskStateRunning
		secondTask.DesiredState = api.TaskStateReady
		require.NoError(t, store.UpdateTask(tx, firstTask))
		require.NoError(t, store.UpdateTask(tx, secondTask))
		return nil
	})
	require.NoError(t, err)

	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))

	s.View(func(tx store.ReadTx) {
		tasks, _ = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	for _, task := range tasks {
		if task.ID == firstTask.ID {
			assert.Equal(t, task.DesiredState, api.TaskStateRunning)
		} else if task.ID == secondTask.ID {
			assert.Equal(t, task.DesiredState, api.TaskStateShutdown)
		} else {
			t.Errorf("Got unexpected task %v in the task listing, should only have gotten %v or %v",
				task.ID, firstTask.ID, secondTask.ID)
		}
	}

	//  3: We create another slot with 2 tasks with desired state READY and expect
	//  one to be shutdown and one to run. The two previously created tasks should
	//  be shutdown.
	err = s.Update(func(tx store.Tx) error {
		// Change the old task's states so that they aren't runnable
		firstTask.DesiredState = api.TaskStateShutdown
		secondTask.DesiredState = api.TaskStateShutdown
		require.NoError(t, store.UpdateTask(tx, firstTask))
		require.NoError(t, store.UpdateTask(tx, secondTask))

		// Create new tasks
		firstTask = orchestrator.NewTask(cluster, service, 1, "")
		firstTask.DesiredState = api.TaskStateReady
		require.NoError(t, store.CreateTask(tx, firstTask))

		secondTask = orchestrator.NewTask(cluster, service, 1, "")
		secondTask.DesiredState = api.TaskStateReady
		require.NoError(t, store.CreateTask(tx, secondTask))
		return nil
	})
	require.NoError(t, err)

	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))

	s.View(func(tx store.ReadTx) {
		tasks, _ = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	firstTaskIndex := 0
	secondTaskIndex := 0
	for i, task := range tasks {
		if task.ID == firstTask.ID {
			firstTaskIndex = i
		} else if task.ID == secondTask.ID {
			secondTaskIndex = i
		}
	}
	assert.True(t, (tasks[firstTaskIndex].DesiredState == api.TaskStateRunning &&
		tasks[secondTaskIndex].DesiredState == api.TaskStateShutdown) ||
		(tasks[secondTaskIndex].DesiredState == api.TaskStateRunning &&
			tasks[firstTaskIndex].DesiredState == api.TaskStateShutdown),
		"Expected one task to be desired RUNNING and the other to be desired SHUTDOWN, but they were in %v and %v",
		tasks[0].DesiredState, tasks[1].DesiredState)

	//  4: We create another slot with a dirty task and a clean
	//  task with desired state READY and expect the first to shutdown
	//  and the second to be run.
	err = s.Update(func(tx store.Tx) error {
		firstTask.DesiredState = api.TaskStateShutdown
		secondTask.DesiredState = api.TaskStateShutdown
		require.NoError(t, store.UpdateTask(tx, firstTask))
		require.NoError(t, store.UpdateTask(tx, secondTask))

		service.Spec.Task.GetContainer().Image = "v:1"
		firstTask = orchestrator.NewTask(cluster, service, 2, "")
		require.NoError(t, store.CreateTask(tx, firstTask))

		service.Spec.Task.GetContainer().Image = "v:2"
		secondTask = orchestrator.NewTask(cluster, service, 2, "")
		secondTask.DesiredState = api.TaskStateReady
		require.NoError(t, store.CreateTask(tx, secondTask))

		return nil
	})
	require.NoError(t, err)

	updater = NewUpdater(s, restart.NewSupervisor(s), cluster, service)
	updater.Run(ctx, getRunnableSlotSlice(t, s, service))

	s.View(func(tx store.ReadTx) {
		tasks, _ = store.FindTasks(tx, store.ByServiceID(service.ID))
	})

	for _, task := range tasks {
		if task.ID == firstTask.ID {
			assert.Equal(t, task.DesiredState, api.TaskStateShutdown)
		} else if task.ID == secondTask.ID {
			assert.Equal(t, task.DesiredState, api.TaskStateRunning)
		}
	}
}
