package controlapi

import (
	"context"
	"strings"
	"testing"

	"github.com/moby/swarmkit/v2/testutils"
	"google.golang.org/grpc/codes"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTask(t *testing.T, ts *testServer, desiredState api.TaskState) *api.Task {
	task := &api.Task{
		ID:           identity.NewID(),
		DesiredState: desiredState,
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{},
			},
		},
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, task)
	})
	require.NoError(t, err)
	return task
}

func TestGetTask(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{})
	require.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: "invalid"})
	require.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))

	task := createTask(t, ts, api.TaskStateRunning)
	r, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: task.ID})
	require.NoError(t, err)
	assert.Equal(t, task.ID, r.Task.ID)
}

func TestRemoveTask(t *testing.T) {
	// TODO
}

func TestListTasks(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	require.NoError(t, err)
	assert.Empty(t, r.Tasks)

	t1 := createTask(t, ts, api.TaskStateRunning)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	require.NoError(t, err)
	assert.Len(t, r.Tasks, 1)

	createTask(t, ts, api.TaskStateRunning)
	createTask(t, ts, api.TaskStateShutdown)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	require.NoError(t, err)
	assert.Len(t, r.Tasks, 3)

	// List with an ID prefix.
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{
		Filters: &api.ListTasksRequest_Filters{
			IDPrefixes: []string{t1.ID[0:4]},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, r.Tasks)
	for _, task := range r.Tasks {
		assert.True(t, strings.HasPrefix(task.ID, t1.ID[0:4]))
	}

	// List by desired state.
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateRunning},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Tasks, 2)
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateShutdown},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Tasks, 1)
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskStateRunning, api.TaskStateShutdown},
			},
		},
	)
	require.NoError(t, err)
	assert.Len(t, r.Tasks, 3)
}
