package controlapi

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/docker/swarmkit/testutils"
	"google.golang.org/grpc/codes"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state/store"
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
	assert.NoError(t, err)
	return task
}

func TestGetTask(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Stop()

	_, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, testutils.ErrorCode(err))

	_, err = ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, testutils.ErrorCode(err))

	task := createTask(t, ts, api.TaskStateRunning)
	r, err := ts.Client.GetTask(context.Background(), &api.GetTaskRequest{TaskID: task.ID})
	assert.NoError(t, err)
	assert.Equal(t, task.ID, r.Task.ID)
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

	t1 := createTask(t, ts, api.TaskStateRunning)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Tasks))

	createTask(t, ts, api.TaskStateRunning)
	createTask(t, ts, api.TaskStateShutdown)
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Tasks))

	// List with an ID prefix.
	r, err = ts.Client.ListTasks(context.Background(), &api.ListTasksRequest{
		Filters: &api.ListTasksRequest_Filters{
			IDPrefixes: []string{t1.ID[0:4]},
		},
	})
	assert.NoError(t, err)
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

func TestListTasksStream(t *testing.T) {
	maxTasksInResponse = 10
	ts := newTestServer(t)
	defer ts.Stop()

	// test that listing with 0 tasks gives us 1 response with 0 tasks in it
	stream, err := ts.Client.ListTasksStream(context.Background(), &api.ListTasksRequest{})
	require.NoError(t, err)

	// we should list twice. once for empty response, and a second time for
	// EOF.
	resp, err := stream.Recv()
	assert.NoError(t, err)
	// require, because otherwise we'll segfault
	require.NotNil(t, resp)
	require.Len(t, resp.Tasks, 0)

	resp, err = stream.Recv()
	assert.Equal(t, err, io.EOF)
	assert.Nil(t, resp)

	// create some tasks
	for i := 0; i < 30; i++ {
		createTask(t, ts, api.TaskStateRunning)
	}

	for numInFinalBatch := 0; numInFinalBatch <= 2; numInFinalBatch++ {
		t.Logf("running with %v tasks in final batch", numInFinalBatch)
		stream, err := ts.Client.ListTasksStream(context.Background(), &api.ListTasksRequest{})
		require.NoError(t, err)

		// in order to make sure we have every tasks (instead of something dumb
		// like getting the same 10 tasks over and over again), we're going to make
		// a set of all of the IDs we've seen
		taskIDs := map[string]struct{}{}

		for i := 0; i < 3; i++ {
			// receive once. should have 10 tasks
			resp, err := stream.Recv()
			assert.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Tasks, 10)

			for _, task := range resp.Tasks {
				assert.NotContains(t, taskIDs, task.ID)
				taskIDs[task.ID] = struct{}{}
			}
		}

		// receive one more time. because the number of tasks aligns to the
		// maxTasksInResponse count, this next messages should contain no tasks
		if numInFinalBatch > 0 {
			resp, err := stream.Recv()
			assert.NoError(t, err)
			require.NotNil(t, resp)
			assert.Len(t, resp.Tasks, numInFinalBatch)
			for _, task := range resp.Tasks {
				assert.NotContains(t, taskIDs, task.ID)
				taskIDs[task.ID] = struct{}{}
			}
		}

		// and now, finally, receive an EOF
		resp, err = stream.Recv()
		assert.Equal(t, err, io.EOF)
		assert.Nil(t, resp)

		// double check we've seen the correct number of tasks
		assert.Len(t, taskIDs, 30+numInFinalBatch)

		// add 1 task
		createTask(t, ts, api.TaskStateRunning)
	}
}
