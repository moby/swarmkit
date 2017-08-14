package update

import (
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/orchestrator/restart"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func getRunnableSlotSlice(t *testing.T, s *store.MemoryStore, service *api.Service) []orchestrator.Slot {
	runnable, _, err := orchestrator.GetRunnableAndDeadSlots(s, service.ID)
	require.NoError(t, err)

	var runnableSlice []orchestrator.Slot
	for _, slot := range runnable {
		runnableSlice = append(runnableSlice, slot)
	}
	return runnableSlice
}

func TestUpdater(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})
	defer cancel()
	go func() {
		for {
			select {
			case e := <-watch:
				task := e.(state.EventUpdateTask).Task
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
		}
	}()

	instances := 3
	cluster := &api.Cluster{
		// test cluster configuration propagation to task creation.
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: "default",
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
						// This won't apply in this test because we set the old tasks to DEAD.
						StopGracePeriod: ptypes.DurationProto(time.Hour),
					},
				},
			},
			Update: &api.UpdateConfig{
				// avoid having Run block for a long time to watch for failures
				Monitor: ptypes.DurationProto(50 * time.Millisecond),
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
		Monitor:     ptypes.DurationProto(50 * time.Millisecond),
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
		Delay:       *ptypes.DurationProto(10 * time.Millisecond),
		Monitor:     ptypes.DurationProto(50 * time.Millisecond),
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
		Delay:       *ptypes.DurationProto(10 * time.Millisecond),
		Monitor:     ptypes.DurationProto(50 * time.Millisecond),
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
}

func TestUpdaterFailureAction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Fail new tasks the updater tries to run
	watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})
	defer cancel()
	go func() {
		for {
			select {
			case e := <-watch:
				task := e.(state.EventUpdateTask).Task
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
		}
	}()

	instances := 3
	cluster := &api.Cluster{
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: "default",
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
						// This won't apply in this test because we set the old tasks to DEAD.
						StopGracePeriod: ptypes.DurationProto(time.Hour),
					},
				},
			},
			Update: &api.UpdateConfig{
				FailureAction: api.UpdateConfig_PAUSE,
				Parallelism:   1,
				Delay:         *ptypes.DurationProto(500 * time.Millisecond),
				Monitor:       ptypes.DurationProto(500 * time.Millisecond),
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

func TestUpdaterStopGracePeriod(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)
	defer s.Close()

	// Move tasks to their desired state.
	watch, cancel := state.Watch(s.WatchQueue(), state.EventUpdateTask{})
	defer cancel()
	go func() {
		for {
			select {
			case e := <-watch:
				task := e.(state.EventUpdateTask).Task
				err := s.Update(func(tx store.Tx) error {
					task = store.GetTask(tx, task.ID)
					// Explicitly do not set task state to
					// DEAD to trigger StopGracePeriod
					if task.DesiredState == api.TaskStateRunning && task.Status.State != api.TaskStateRunning {
						task.Status.State = api.TaskStateRunning
						return store.UpdateTask(tx, task)
					}
					return nil
				})
				assert.NoError(t, err)
			}
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
						Image:           "v:1",
						StopGracePeriod: ptypes.DurationProto(100 * time.Millisecond),
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
				Monitor: ptypes.DurationProto(50 * time.Millisecond),
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
