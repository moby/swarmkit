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
)

func createTask(t *testing.T, ts *testServer, desiredState api.TaskState) *api.Task {
	task := &api.Task{
		Id:           identity.NewID(),
		DesiredState: desiredState,
		Spec: &api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{},
			},
		},
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, task)
	})
	assert.NoError(t, err)
	return task
}

func TestGetTask(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskId: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))

	task := createTask(t, ts, api.TaskState_RUNNING)
	r, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskId: task.Id})
	assert.NoError(t, err)
	assert.Equal(t, task.Id, r.Task.Id)
}

func TestRemoveTask(t *testing.T) {
	// TODO
}

func TestListTasks(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()
	r, err := ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Tasks)

	t1 := createTask(t, ts, api.TaskState_RUNNING)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Tasks))

	createTask(t, ts, api.TaskState_RUNNING)
	createTask(t, ts, api.TaskState_SHUTDOWN)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Tasks))

	// List with an ID prefix.
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{
		Filters: &api.ListTasksRequest_Filters{
			IdPrefixes: []string{t1.Id[0:4]},
		},
	})
	assert.NoError(t, err)
	assert.NotEmpty(t, r.Tasks)
	for _, task := range r.Tasks {
		assert.True(t, strings.HasPrefix(task.Id, t1.Id[0:4]))
	}

	// List by desired state.
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskState_RUNNING},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Tasks))
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskState_SHUTDOWN},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Tasks))
	r, err = ts.Client.ListTasks(context.Background(),
		&api.ListTasksRequest{
			Filters: &api.ListTasksRequest_Filters{
				DesiredStates: []api.TaskState{api.TaskState_RUNNING, api.TaskState_SHUTDOWN},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Tasks))
}
