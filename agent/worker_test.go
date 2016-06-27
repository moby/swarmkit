package agent

import (
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestWorker(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	executor := &mockExecutor{t: t}
	worker := newWorker(db, executor)
	reporter := statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		log.G(ctx).WithFields(logrus.Fields{"task.id": taskID, "status": status}).Info("status update received")
		return nil
	})

	worker.Listen(ctx, reporter)

	for _, testcase := range []struct {
		taskSet          []*api.Task
		expectedTasks    []*api.Task
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

		// TODO(stevvooe): There are a few more states here we need to get
		// covered to ensure correct during code changes.
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
	t *testing.T
	exec.Controller
	task *api.Task
}

func (mtc *mockTaskController) Remove(ctx context.Context) error {
	mtc.t.Log("(*mockTestController).Remove")
	return nil
}

func (mtc *mockTaskController) Close() error {
	mtc.t.Log("(*mockTestController).Close")
	return nil
}

type mockExecutor struct {
	t *testing.T
	exec.Executor
}

func (m *mockExecutor) Controller(task *api.Task) (exec.Controller, error) {
	return &mockTaskController{t: m.t, task: task}, nil
}
