package agent

import (
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/boltdb/bolt"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/agent/secrets"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

type testPublisherProvider struct {
}

func (tpp *testPublisherProvider) Publisher(ctx context.Context, subscriptionID string) (exec.LogPublisher, func(), error) {
	return exec.LogPublisherFunc(func(ctx context.Context, message api.LogMessage) error {
			log.G(ctx).WithFields(logrus.Fields{
				"subscription": subscriptionID,
				"task.id":      message.Context.TaskID,
				"node.id":      message.Context.NodeID,
				"service.id":   message.Context.ServiceID,
			}).Info(message.Data)
			return nil
		}), func() {
		}, nil
}

func TestWorkerAssign(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	executor := &mockExecutor{t: t, secrets: secrets.NewManager()}
	worker := newWorker(db, executor, &testPublisherProvider{})
	reporter := statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		log.G(ctx).WithFields(logrus.Fields{"task.id": taskID, "status": status}).Info("status update received")
		return nil
	})

	worker.Listen(ctx, reporter)

	for _, testcase := range []struct {
		changeSet        []*api.AssignmentChange
		expectedTasks    []*api.Task
		expectedSecrets  []*api.Secret
		expectedAssigned []*api.Task
	}{
		{}, // handle nil case.
		{
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
				// these should be ignored
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-1"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-1"},
			},
		},
		{ // completely replaces the existing tasks and secrets
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-2"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-2"},
			},
		},
		{
			// remove assigned tasks, secret no longer present
			expectedTasks: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
		},

		// TODO(stevvooe): There are a few more states here we need to get
		// covered to ensure correct during code changes.
	} {
		assert.NoError(t, worker.Assign(ctx, testcase.changeSet))

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
		for _, secret := range testcase.expectedSecrets {
			assert.NotNil(t, executor.secrets.Get(secret.ID))
		}
	}
}

func TestWorkerUpdate(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()
	executor := &mockExecutor{t: t, secrets: secrets.NewManager()}
	worker := newWorker(db, executor, &testPublisherProvider{})
	reporter := statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		log.G(ctx).WithFields(logrus.Fields{"task.id": taskID, "status": status}).Info("status update received")
		return nil
	})

	worker.Listen(ctx, reporter)

	// create an existing task and secret
	assert.NoError(t, worker.Assign(ctx, []*api.AssignmentChange{
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Task{
					Task: &api.Task{ID: "task-1"},
				},
			},
			Action: api.AssignmentChange_AssignmentActionUpdate,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Secret{
					Secret: &api.Secret{ID: "secret-1"},
				},
			},
			Action: api.AssignmentChange_AssignmentActionUpdate,
		},
	}))

	for _, testcase := range []struct {
		changeSet        []*api.AssignmentChange
		expectedTasks    []*api.Task
		expectedSecrets  []*api.Secret
		expectedAssigned []*api.Task
	}{
		{ // handle nil changeSet case.
			expectedTasks: []*api.Task{
				{ID: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-1"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-1"},
			},
		},
		{
			// no changes
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-1"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-1"},
			},
		},
		{
			// adding a secret and task
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-1"},
				{ID: "secret-2"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
		},
		{
			// remove assigned task and secret, updating existing secret
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionUpdate,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{ID: "secret-2"},
			},
			expectedAssigned: []*api.Task{
				{ID: "task-2"},
			},
		},
		{
			// removing nonexistent items doesn't fail
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-1"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{ID: "task-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{ID: "secret-2"},
						},
					},
					Action: api.AssignmentChange_AssignmentActionRemove,
				},
			},
			expectedTasks: []*api.Task{
				{ID: "task-1"},
				{ID: "task-2"},
			},
		},
	} {
		assert.NoError(t, worker.Update(ctx, testcase.changeSet))

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
		for _, secret := range testcase.expectedSecrets {
			assert.NotNil(t, executor.secrets.Get(secret.ID))
		}
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
	secrets exec.SecretsManager
}

func (m *mockExecutor) Controller(task *api.Task) (exec.Controller, error) {
	return &mockTaskController{t: m.t, task: task}, nil
}

func (m *mockExecutor) Secrets() exec.SecretsManager {
	return m.secrets
}
