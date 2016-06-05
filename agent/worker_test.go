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
	factory := &mockTaskManagerFactory{}

	worker := newWorker(context.Background(), db, factory, statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		log.G(ctx).WithFields(logrus.Fields{"task.id": taskID, "status": status}).Info("status update received")
		return nil
	}))

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

		assert.NoError(t, worker.db.View(func(tx *bolt.Tx) error {
			tasks := GetTasks(tx)
			assert.Equal(t, testcase.expectedTasks, tasks)

			var assigned []*api.Task
			for _, task := range tasks {
				if TaskAssigned(tx, task.ID) {
					assigned = append(assigned, task)
				}
			}

			assert.Equal(t, testcase.expectedAssigned, assigned)

			return nil
		}))
	}
}

type mockTaskController struct {
	exec.Controller
}

type mockTaskManagerFactory struct {
	TaskManagerFactory
}

func (*mockTaskManagerFactory) TaskManager(ctx context.Context, task *api.Task, reporter StatusReporter) TaskManager {
	return newTaskManager(ctx, task, &mockTaskController{}, reporter)
}
