package agent

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/moby/swarmkit/v2/agent/exec"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/log"
	"github.com/moby/swarmkit/v2/testutils"
	"github.com/stretchr/testify/assert"
	bolt "go.etcd.io/bbolt"
)

type testPublisherProvider struct {
}

func (tpp *testPublisherProvider) Publisher(ctx context.Context, subscriptionID string) (exec.LogPublisher, func(), error) {
	return exec.LogPublisherFunc(func(ctx context.Context, message *api.LogMessage) error {
			log.G(ctx).WithFields(log.Fields{
				"subscription": subscriptionID,
				"task.id":      message.Context.TaskId,
				"node.id":      message.Context.NodeId,
				"service.id":   message.Context.ServiceId,
			}).Info(message.Data)
			return nil
		}), func() {
		}, nil
}

func newFakeReporter(statuses statusReporterFunc, volumes volumeReporterFunc) Reporter {
	return statusReporterCombined{
		statusReporterFunc: statuses,
		volumeReporterFunc: volumes,
	}
}

func TestWorkerAssign(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	pg := &testutils.FakePluginGetter{
		Plugins: map[string]*testutils.FakePlugin{
			"plugin-1": {
				PluginName: "plugin-1",
				PluginAddr: &net.UnixAddr{},
			},
			"plugin-2": {
				PluginName: "plugin-2",
				PluginAddr: &net.UnixAddr{},
			},
		},
	}

	ctx := context.Background()
	executor := &mockExecutor{dependencies: NewDependencyManager(pg)}

	worker := newWorker(db, executor, &testPublisherProvider{})
	reporter := newFakeReporter(
		statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
			log.G(ctx).WithFields(log.Fields{"task.id": taskID, "status": status}).Info("status update received")
			return nil
		}),
		volumeReporterFunc(func(ctx context.Context, volumeID string) error {
			return nil
		}),
	)

	worker.Listen(ctx, reporter)

	for _, testcase := range []struct {
		changeSet        []*api.AssignmentChange
		expectedTasks    []*api.Task
		expectedSecrets  []*api.Secret
		expectedConfigs  []*api.Config
		expectedAssigned []*api.Task
		expectedVolumes  []*api.VolumeAssignment
	}{
		{}, // handle nil case.
		{
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-1"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-1"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-1"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				// these should be ignored
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
			},
			expectedTasks: []*api.Task{
				{Id: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-1"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-1"},
			},
			expectedAssigned: []*api.Task{
				{Id: "task-1"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
			},
		},
		{ // completely replaces the existing tasks and secrets
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
			},
			expectedTasks: []*api.Task{
				{Id: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-2"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-2"},
			},
			expectedAssigned: []*api.Task{
				// task-1 should be cleaned up and deleted.
				{Id: "task-2"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
			},
		},
		{
			// remove assigned tasks, secret and config no longer present
			// there should be no tasks in the tasks db after this.
			expectedTasks: nil,
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
				if TaskAssigned(tx, task.Id) {
					assigned = append(assigned, task)
				}
				return nil
			})
		}))

		assertTasksEqual(t, testcase.expectedTasks, tasks)
		assertTasksEqual(t, testcase.expectedAssigned, assigned)
		for _, secret := range testcase.expectedSecrets {
			secret, err := executor.Secrets().Get(secret.Id)
			assert.NoError(t, err)
			assert.NotNil(t, secret)
		}
		for _, config := range testcase.expectedConfigs {
			config, err := executor.Configs().Get(config.Id)
			assert.NoError(t, err)
			assert.NotNil(t, config)
		}
		for _, volume := range testcase.expectedVolumes {
			_, err := executor.Volumes().Get(volume.VolumeId)
			assert.Error(t, err)
			assert.True(t, errors.Is(err, exec.ErrDependencyNotReady))
		}
	}
}

func TestWorkerWait(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	pg := &testutils.FakePluginGetter{
		Plugins: map[string]*testutils.FakePlugin{
			"plugin-1": {
				PluginName: "plugin-1",
				PluginAddr: &net.UnixAddr{},
			},
		},
	}

	executor := &mockExecutor{dependencies: NewDependencyManager(pg)}

	worker := newWorker(db, executor, &testPublisherProvider{})
	reporter := newFakeReporter(
		statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
			log.G(ctx).WithFields(log.Fields{"task.id": taskID, "status": status}).Info("status update received")
			return nil
		}),
		volumeReporterFunc(func(ctx context.Context, volumeID string) error {
			return nil
		}),
	)

	worker.Listen(ctx, reporter)

	changeSet := []*api.AssignmentChange{
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Task{
					Task: &api.Task{Id: "task-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Task{
					Task: &api.Task{Id: "task-2"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Secret{
					Secret: &api.Secret{Id: "secret-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Config{
					Config: &api.Config{Id: "config-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Volume{
					Volume: &api.VolumeAssignment{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
	}

	expectedTasks := []*api.Task{
		{Id: "task-1"},
		{Id: "task-2"},
	}

	expectedSecrets := []*api.Secret{
		{Id: "secret-1"},
	}

	expectedConfigs := []*api.Config{
		{Id: "config-1"},
	}

	expectedAssigned := []*api.Task{
		{Id: "task-1"},
		{Id: "task-2"},
	}

	expectedVolumes := []*api.VolumeAssignment{
		{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
	}

	var (
		tasks    []*api.Task
		assigned []*api.Task
	)
	assert.NoError(t, worker.Assign(ctx, changeSet))

	assert.NoError(t, worker.db.View(func(tx *bolt.Tx) error {
		return WalkTasks(tx, func(task *api.Task) error {
			tasks = append(tasks, task)
			if TaskAssigned(tx, task.Id) {
				assigned = append(assigned, task)
			}
			return nil
		})
	}))

	assertTasksEqual(t, expectedTasks, tasks)
	assertTasksEqual(t, expectedAssigned, assigned)
	for _, secret := range expectedSecrets {
		secret, err := executor.Secrets().Get(secret.Id)
		assert.NoError(t, err)
		assert.NotNil(t, secret)
	}
	for _, config := range expectedConfigs {
		config, err := executor.Configs().Get(config.Id)
		assert.NoError(t, err)
		assert.NotNil(t, config)
	}
	for _, volume := range expectedVolumes {
		_, err := executor.Volumes().Get(volume.VolumeId)
		assert.Error(t, err)
		assert.True(t, errors.Is(err, exec.ErrDependencyNotReady))
	}

	err := worker.Assign(ctx, nil)
	assert.Nil(t, err)

	err = worker.Wait(ctx)
	assert.Nil(t, err)

	assigned = assigned[:0]

	assert.NoError(t, worker.db.View(func(tx *bolt.Tx) error {
		return WalkTasks(tx, func(task *api.Task) error {
			if TaskAssigned(tx, task.Id) {
				assigned = append(assigned, task)
			}
			return nil
		})
	}))
	assert.Equal(t, len(assigned), 0)
}

func TestWorkerUpdate(t *testing.T) {
	db, cleanup := storageTestEnv(t)
	defer cleanup()

	ctx := context.Background()

	pg := &testutils.FakePluginGetter{
		Plugins: map[string]*testutils.FakePlugin{
			"plugin-1": {
				PluginName: "plugin-1",
				PluginAddr: &net.UnixAddr{},
			},
			"plugin-2": {
				PluginName: "plugin-2",
				PluginAddr: &net.UnixAddr{},
			},
		},
	}

	executor := &mockExecutor{dependencies: NewDependencyManager(pg)}
	worker := newWorker(db, executor, &testPublisherProvider{})
	reporter := newFakeReporter(
		statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
			log.G(ctx).WithFields(log.Fields{"task.id": taskID, "status": status}).Info("status update received")
			return nil
		}),
		volumeReporterFunc(func(ctx context.Context, volumeID string) error {
			return nil
		}),
	)

	worker.Listen(ctx, reporter)

	// create existing task/secret/config/volume
	assert.NoError(t, worker.Assign(ctx, []*api.AssignmentChange{
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Task{
					Task: &api.Task{Id: "task-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Secret{
					Secret: &api.Secret{Id: "secret-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Config{
					Config: &api.Config{Id: "config-1"},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
		{
			Assignment: &api.Assignment{
				Item: &api.Assignment_Volume{
					Volume: &api.VolumeAssignment{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
				},
			},
			Action: api.AssignmentChange_UPDATE,
		},
	}))

	for _, testcase := range []struct {
		changeSet        []*api.AssignmentChange
		expectedTasks    []*api.Task
		expectedSecrets  []*api.Secret
		expectedConfigs  []*api.Config
		expectedAssigned []*api.Task
		expectedVolumes  []*api.VolumeAssignment
	}{
		{ // handle nil changeSet case.
			expectedTasks: []*api.Task{
				{Id: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-1"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-1"},
			},
			expectedAssigned: []*api.Task{
				{Id: "task-1"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
			},
		},
		{
			// no changes
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-1"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
			},
			expectedTasks: []*api.Task{
				{Id: "task-1"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-1"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-1"},
			},
			expectedAssigned: []*api.Task{
				{Id: "task-1"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
			},
		},
		{
			// adding a secret and task
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
			},
			expectedTasks: []*api.Task{
				{Id: "task-1"},
				{Id: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-1"},
				{Id: "secret-2"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-1"},
				{Id: "config-2"},
			},
			expectedAssigned: []*api.Task{
				{Id: "task-1"},
				{Id: "task-2"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
				{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
			},
		},
		{
			// remove assigned task and secret, updating existing secret
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-2"},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
						},
					},
					Action: api.AssignmentChange_UPDATE,
				},
			},
			expectedTasks: []*api.Task{
				{Id: "task-2"},
			},
			expectedSecrets: []*api.Secret{
				{Id: "secret-2"},
			},
			expectedConfigs: []*api.Config{
				{Id: "config-2"},
			},
			expectedAssigned: []*api.Task{
				{Id: "task-2"},
			},
			expectedVolumes: []*api.VolumeAssignment{
				{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
			},
		},
		{
			// removing nonexistent items doesn't fail
			changeSet: []*api.AssignmentChange{
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Task{
							Task: &api.Task{Id: "task-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Secret{
							Secret: &api.Secret{Id: "secret-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-1"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Config{
							Config: &api.Config{Id: "config-2"},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID1", VolumeId: "volume-1", Driver: &api.Driver{Name: "plugin-1"}},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
				{
					Assignment: &api.Assignment{
						Item: &api.Assignment_Volume{
							Volume: &api.VolumeAssignment{Id: "volumeID2", VolumeId: "volume-2", Driver: &api.Driver{Name: "plugin-2"}},
						},
					},
					Action: api.AssignmentChange_REMOVE,
				},
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
				if TaskAssigned(tx, task.Id) {
					assigned = append(assigned, task)
				}
				return nil
			})
		}))

		assertTasksEqual(t, testcase.expectedTasks, tasks)
		assertTasksEqual(t, testcase.expectedAssigned, assigned)
		for _, secret := range testcase.expectedSecrets {
			secret, err := executor.Secrets().Get(secret.Id)
			assert.NoError(t, err)
			assert.NotNil(t, secret)
		}
		for _, config := range testcase.expectedConfigs {
			config, err := executor.Configs().Get(config.Id)
			assert.NoError(t, err)
			assert.NotNil(t, config)
		}
		for _, volume := range testcase.expectedVolumes {
			_, err := executor.Volumes().Get(volume.VolumeId)
			// volumes should not be ready yet, so we expect an error.
			assert.Error(t, err)
			assert.True(t, errors.Is(err, exec.ErrDependencyNotReady), "error: %v", err)
		}
	}
}

type mockTaskController struct {
	exec.Controller
	task         *api.Task
	dependencies exec.DependencyGetter
}

func (mtc *mockTaskController) Remove(ctx context.Context) error {
	return nil
}

func (mtc *mockTaskController) Close() error {
	return nil
}

type mockExecutor struct {
	exec.Executor
	dependencies exec.DependencyManager
}

func (m *mockExecutor) Controller(task *api.Task) (exec.Controller, error) {
	return &mockTaskController{task: task, dependencies: Restrict(m.dependencies, task)}, nil
}

func (m *mockExecutor) Secrets() exec.SecretsManager {
	return m.dependencies.Secrets()
}

func (m *mockExecutor) Configs() exec.ConfigsManager {
	return m.dependencies.Configs()
}

func (m *mockExecutor) Volumes() exec.VolumesManager {
	return m.dependencies.Volumes()
}

// assertTasksEqual compares two task slices with protobuf semantics.
// assert.Equal cannot be used: it falls back to reflect.DeepEqual, which walks
// the messages' internal state and reports two equal messages as different
// once one of them has been marshalled.
func assertTasksEqual(t *testing.T, expected, actual []*api.Task) {
	t.Helper()
	if !assert.Len(t, actual, len(expected)) {
		return
	}
	for i := range expected {
		assert.True(t, expected[i].EqualVT(actual[i]), "task %d differs:\n want %v\n  got %v", i, expected[i], actual[i])
	}
}
