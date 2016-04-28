package container

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"runtime"
	"testing"

	"github.com/docker/engine-api/types"
	"github.com/docker/engine-api/types/events"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

// TODO(stevvooe): Generation of mocks against circle ci is broken. If you need
// to regenerate the mock, remove the "+" below and run `go generate`. Sorry.
//+go:generate mockgen -package dockerexec -destination api_client_test.mock.go github.com/docker/engine-api/client APIClient

func TestRunnerPrepare(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerCreate(ctx, config.config(), config.hostConfig(), config.networkingConfig(), config.name()).
			Return(types.ContainerCreateResponse{ID: "contianer-id-" + task.ID}, nil),
	)

	assert.NoError(t, runner.Prepare(ctx))
}

func TestRunnerPrepareAlreadyPrepared(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerCreate(
			ctx, config.config(), config.hostConfig(), config.networkingConfig(), config.name()).
			Return(types.ContainerCreateResponse{}, fmt.Errorf("Conflict. The name")),
		client.EXPECT().ContainerInspect(ctx, config.name()).
			Return(types.ContainerJSON{}, nil),
	)

	// ensure idempotence
	if err := runner.Prepare(ctx); err != exec.ErrTaskPrepared {
		t.Fatalf("expected error %v, got %v", exec.ErrTaskPrepared, err)
	}
}

func TestRunnerStart(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerInspect(ctx, config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "created",
					},
				},
			}, nil),
		client.EXPECT().ContainerStart(ctx, config.name()).
			Return(nil),
	)

	assert.NoError(t, runner.Start(ctx))
}

func TestRunnerStartAlreadyStarted(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerInspect(ctx, config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "notcreated", // can be anything but created
					},
				},
			}, nil),
	)

	// ensure idempotence
	if err := runner.Start(ctx); err != exec.ErrTaskStarted {
		t.Fatalf("expected error %v, got %v", exec.ErrTaskPrepared, err)
	}
}

func TestRunnerWait(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().Events(gomock.Any(), types.EventsOptions{
			Since:   "0",
			Filters: config.eventFilter(),
		}).Return(makeEvents(t, config, "create", "die"), nil),
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "stopped", // can be anything but created
					},
				},
			}, nil),
	)

	assert.NoError(t, runner.Wait(ctx))
}

func TestRunnerWaitExitError(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().Events(gomock.Any(), types.EventsOptions{
			Since:   "0",
			Filters: config.eventFilter(),
		}).Return(makeEvents(t, config, "create", "die"), nil),
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status:   "exited", // can be anything but created
						ExitCode: 1,
					},
				},
			}, nil),
	)

	err := runner.Wait(ctx)
	assert.Equal(t, &exec.ExitError{
		Code: 1,
	}, err)
}

func TestRunnerShutdown(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerStop(gomock.Any(), config.name(), 10),
	)

	assert.NoError(t, runner.Shutdown(ctx))
}

func TestRunnerTerminate(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	client.EXPECT().ContainerKill(gomock.Any(), config.name(), "")

	assert.NoError(t, runner.Terminate(ctx))
}

func TestRunnerRemove(t *testing.T) {
	task := genTask(t)
	ctx, client, runner, config, finish := genTestRunnerEnv(t, task)
	defer finish(t)

	client.EXPECT().ContainerRemove(gomock.Any(), types.ContainerRemoveOptions{
		ContainerID:   config.name(),
		RemoveVolumes: true,
		Force:         true,
	})

	assert.NoError(t, runner.Remove(ctx))
}

func genTestRunnerEnv(t *testing.T, task *api.Task) (context.Context, *MockAPIClient, *Runner, *containerConfig, func(t *testing.T)) {
	mocks := gomock.NewController(t)
	client := NewMockAPIClient(mocks)
	runner, err := NewRunner(client, task)
	assert.NoError(t, err)

	config, err := newContainerConfig(task)
	assert.NoError(t, err)
	assert.NotNil(t, config)

	ctx := context.Background()

	// Put test name into log messages. Awesome!
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		ctx = log.WithLogger(ctx, log.L.WithField("test", fn.Name()))
	}

	ctx, cancel := context.WithCancel(ctx)
	return ctx, client, runner, config, func(t *testing.T) {
		cancel()
		mocks.Finish()
	}
}

func genTask(t *testing.T) *api.Task {
	const (
		nodeID    = "dockerexec-test-node-id"
		serviceID = "dockerexec-test-service"
		reference = "stevvooe/foo:latest"
	)

	return &api.Task{
		ID:        identity.NewID(),
		ServiceID: serviceID,
		NodeID:    nodeID,
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.Container{
					Image: &api.Image{
						Reference: reference,
					},
				},
			},
		},
	}
}

func makeEvents(t *testing.T, container *containerConfig, actions ...string) io.ReadCloser {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)

	for _, action := range actions {
		event := events.Message{
			Type:   events.ContainerEventType,
			Action: action,
			Actor: events.Actor{
				// TODO(stevvooe): Resolve container id.
				Attributes: map[string]string{
					"name": container.name(),
				},
			},
		}

		if err := enc.Encode(event); err != nil {
			t.Fatalf("error preparing events: %v (encoding %v)", err, event)
		}
	}

	return ioutil.NopCloser(&buf)
}
