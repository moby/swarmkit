package controlapi

import (
	"testing"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func createTask(t *testing.T, ts *testServer, id string, desiredState api.TaskState) *api.Task {
	task := &api.Task{
		ID:           id,
		DesiredState: desiredState,
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, task)
	})
	assert.NoError(t, err)
	return task
}

func TestGetTask(t *testing.T) {
	ts := newTestServer(t)

	_, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	task := createTask(t, ts, "id", api.TaskStateRunning)
	r, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: task.ID})
	assert.NoError(t, err)
	assert.Equal(t, task.ID, r.Task.ID)
}

func TestRemoveTask(t *testing.T) {
	// TODO
}

func TestListTasks(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Tasks)

	createTask(t, ts, "id1", api.TaskStateRunning)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Tasks))

	createTask(t, ts, "id2", api.TaskStateRunning)
	createTask(t, ts, "id3", api.TaskStateShutdown)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Tasks))

	// List by desired state.
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateRunning},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Tasks))
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateShutdown},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Tasks))
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateRunning, api.TaskStateShutdown},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Tasks))
}
