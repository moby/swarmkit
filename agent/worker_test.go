package agent

import (
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestWorker(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	executor := &mockExecutor{}
	worker := newWorker(db, executor)
	reporter := statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		log.G(ctx).WithFields(logrus.Fields{"task.id": taskID, "status": status}).Info("status update received")
		return nil
	})

	worker.Listen(ctx, reporter)

	for _, testcase := range []struct {
		taskSet          []*api.Task
		expectedTasks    []*api.Task
		expectedStatuses []*api.TaskStatus
		expectedAssigned []*api.Task
	}{
		{}, // handle nil case.
		{
			taskSet: []*api.Task{
				{ID: "task-1"},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-1"},
			},
		},
		{
			// remove assigned tasks
			expectedTasks: []*api.Task{
				{ID: "task-1"},
			},
		},
	} {
		assert.NoError(t, worker.Assign(ctx, testcase.taskSet))

		var (
			tasks    []*api.Task
			assigned []*api.Task
		)
		assert.NoError(t, worker.db.View(func(tx *bolt.Tx) error {
			return WalkTasks(tx, func(task *api.Task) error {
				tasks = append(tasks, task)
				if TaskAssigned(tx, task.ID) {
					assigned = append(assigned, task)
				}
				return nil
			})
		}))

		assert.Equal(t, testcase.expectedTasks, tasks)
		assert.Equal(t, testcase.expectedAssigned, assigned)
	}
}

type mockTaskController struct {
	exec.Controller
	task *api.Task
}

type mockExecutor struct {
	exec.Executor
}

func (*mockExecutor) Controller(task *api.Task) (exec.Controller, error) {
	return &mockTaskController{task: task}, nil
}
