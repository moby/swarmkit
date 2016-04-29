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

func getRunnableServiceTasks(t *testing.T, store state.WatchableStore, s *api.Service) []*api.Task {
	var (
		err   error
		tasks []*api.Task
	)

	err = store.View(func(tx state.ReadTx) error {
		tasks, err = tx.Tasks().Find(state.ByServiceID(s.ID))
		return err
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
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	// Move tasks to their desired state.
	watch, cancel := state.Watch(store.WatchQueue(), state.EventCreateTask{})
	defer cancel()
	go func() {
		for {
			select {
			case e := <-watch:
				task := e.(state.EventCreateTask).Task
				err := store.Update(func(tx state.Tx) error {
					task = tx.Tasks().Get(task.ID)
					if task.DesiredState == api.TaskStateRunning {
						task.Status.State = api.TaskStateRunning
						return tx.Tasks().Update(task)
					}
					return nil
				})
				assert.NoError(t, err)
			}
		}
	}()

	service := &api.Service{
		ID: "id1",
		Spec: api.ServiceSpec{
			Annotations: api.Annotations{
				Name: "name1",
			},
			Template: &api.TaskSpec{
				Runtime: &api.TaskSpec_Container{
					Container: &api.Container{
						Image: &api.Image{
							Reference: "v:1",
						},
					},
				},
			},
			Instances: 3,
			Mode:      api.ServiceModeRunning,
		},
	}

	err := store.Update(func(tx state.Tx) error {
		assert.NoError(t, tx.Services().Create(service))
		for i := int64(0); i < service.Spec.Instances; i++ {
			assert.NoError(t, tx.Tasks().Create(newTask(service, uint64(i))))
		}
		return nil
	})
	assert.NoError(t, err)

	originalTasks := getRunnableServiceTasks(t, store, service)
	for _, task := range originalTasks {
		assert.Equal(t, "v:1", task.Spec.GetContainer().Image.Reference)
	}

	service.Spec.Template.GetContainer().Image.Reference = "v:2"
	updater := NewUpdater(store)
	updater.Run(ctx, service, getRunnableServiceTasks(t, store, service))
	updatedTasks := getRunnableServiceTasks(t, store, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:2", task.Spec.GetContainer().Image.Reference)
	}

	service.Spec.Template.GetContainer().Image.Reference = "v:3"
	service.Spec.Update = &api.UpdateConfiguration{
		Parallelism: 1,
	}
	updater = NewUpdater(store)
	updater.Run(ctx, service, getRunnableServiceTasks(t, store, service))
	updatedTasks = getRunnableServiceTasks(t, store, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:3", task.Spec.GetContainer().Image.Reference)
	}

	service.Spec.Template.GetContainer().Image.Reference = "v:4"
	service.Spec.Update = &api.UpdateConfiguration{
		Parallelism: 1,
		Delay:       10 * time.Millisecond,
	}
	updater = NewUpdater(store)
	updater.Run(ctx, service, getRunnableServiceTasks(t, store, service))
	updatedTasks = getRunnableServiceTasks(t, store, service)
	for _, task := range updatedTasks {
		assert.Equal(t, "v:4", task.Spec.GetContainer().Image.Reference)
	}
}
