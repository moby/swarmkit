package orchestrator

import (
	"testing"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func getRunnableServiceTasks(t *testing.T, s *store.MemoryStore, service *api.Service) []*api.Task {
	var (
		err   error
		tasks []*api.Task
	)

	s.View(func(tx store.ReadTx) {
		tasks, err = store.FindTasks(tx, store.ByServiceID(service.ID))
	})
	assert.NoError(t, err)

	runnable := []*api.Task{}
	for _, task := range tasks {
		if task.DesiredState == api.TaskStateRunning {
			runnable = append(runnable, task)
		}
	}
	return runnable
}

func TestUpdater(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)

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
	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			RuntimeSpec: &api.ServiceSpec_Container{
				Container: &api.ContainerSpec{
					Image: api.Image{
						Reference: "v:1",
					},
					// This won't apply in this test because we set the old tasks to DEAD.
					StopGracePeriod: time.Hour,
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Instances: uint64(instances),
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service))
		for i := 0; i < instances; i++ {
			assert.NoError(t, store.CreateTask(tx, newTask(service, uint64(i))))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableServiceTasks(t, s, service)
	for _, task := range originalTasks {
		assert.Equal(t, "v:1", task.GetContainer().Spec.Image.Reference)
	}

	service.Spec.GetContainer().Image.Reference = "v:2"
	updater := NewUpdater(s, NewRestartSupervisor(s))
	updater.Run(ctx, service, getRunnableServiceTasks(t, s, service))
	updatedTasks := getRunnableServiceTasks(t, s, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:2", task.GetContainer().Spec.Image.Reference)
	}

	service.Spec.GetContainer().Image.Reference = "v:3"
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
	}
	updater = NewUpdater(s, NewRestartSupervisor(s))
	updater.Run(ctx, service, getRunnableServiceTasks(t, s, service))
	updatedTasks = getRunnableServiceTasks(t, s, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:3", task.GetContainer().Spec.Image.Reference)
	}

	service.Spec.GetContainer().Image.Reference = "v:4"
	service.Spec.Update = &api.UpdateConfig{
		Parallelism: 1,
		Delay:       10 * time.Millisecond,
	}
	updater = NewUpdater(s, NewRestartSupervisor(s))
	updater.Run(ctx, service, getRunnableServiceTasks(t, s, service))
	updatedTasks = getRunnableServiceTasks(t, s, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:4", task.GetContainer().Spec.Image.Reference)
	}
}

func TestUpdaterStopGracePeriod(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	assert.NotNil(t, s)

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
			RuntimeSpec: &api.ServiceSpec_Container{
				Container: &api.ContainerSpec{
					Image: api.Image{
						Reference: "v:1",
					},
					StopGracePeriod: 100 * time.Millisecond,
				},
			},
			Mode: &api.ServiceSpec_Replicated{
				Replicated: &api.ReplicatedService{
					Instances: instances,
				},
			},
		},
	}

	err := s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateService(tx, service))
		for i := uint64(0); i < instances; i++ {
			task := newTask(service, uint64(i))
			task.Status.State = api.TaskStateRunning
			assert.NoError(t, store.CreateTask(tx, task))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableServiceTasks(t, s, service)
	for _, task := range originalTasks {
		assert.Equal(t, "v:1", task.GetContainer().Spec.Image.Reference)
	}

	before := time.Now()

	service.Spec.GetContainer().Image.Reference = "v:2"
	updater := NewUpdater(s, NewRestartSupervisor(s))
	updater.Run(ctx, service, getRunnableServiceTasks(t, s, service))
	updatedTasks := getRunnableServiceTasks(t, s, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:2", task.GetContainer().Spec.Image.Reference)
	}

	after := time.Now()

	// At least 100 ms should have elapsed. Only check the lower bound,
	// because the system may be slow and it could have taken longer.
	if after.Sub(before) < 100*time.Millisecond {
		t.Fatal("stop timeout should have elapsed")
	}
}
