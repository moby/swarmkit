package dockerapi

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"runtime"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	containertypes "github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/events"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/log"
	gogotypes "github.com/gogo/protobuf/types"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var tenSecond = 10 * time.Second

// TODO(stevvooe): Generation of mocks against circle ci is broken. If you need
// to regenerate the mock, remove the "+" below and run `go generate`. Sorry.
// UPDATE(stevvooe): Gomock is still broken garbage. Sigh. This time, had to
// generate, then manually "unvendor" imports. Further cements the
// realization that mocks are a garbage way to build tests.
//+go:generate mockgen -package dockerapi -destination api_client_test.mock.go github.com/docker/docker/client APIClient

func TestControllerPrepare(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ImagePull(gomock.Any(), config.image(), gomock.Any()).
			Return(ioutil.NopCloser(bytes.NewBuffer([]byte{})), nil),
		client.EXPECT().ContainerCreate(gomock.Any(), config.config(), config.hostConfig(), config.networkingConfig(), config.name()).
			Return(containertypes.ContainerCreateCreatedBody{ID: "container-id-" + task.ID}, nil),
	)

	assert.NoError(t, ctlr.Prepare(ctx))
}

func TestControllerPrepareAlreadyPrepared(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ImagePull(gomock.Any(), config.image(), gomock.Any()).
			Return(ioutil.NopCloser(bytes.NewBuffer([]byte{})), nil),
		client.EXPECT().ContainerCreate(
			ctx, config.config(), config.hostConfig(), config.networkingConfig(), config.name()).
			Return(containertypes.ContainerCreateCreatedBody{}, fmt.Errorf("Conflict. The name")),
		client.EXPECT().ContainerInspect(ctx, config.name()).
			Return(types.ContainerJSON{}, nil),
	)

	// ensure idempotence
	if err := ctlr.Prepare(ctx); err != exec.ErrTaskPrepared {
		t.Fatalf("expected error %v, got %v", exec.ErrTaskPrepared, err)
	}
}

func TestControllerStart(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
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
		client.EXPECT().ContainerStart(ctx, config.name(), types.ContainerStartOptions{}).
			Return(nil),
	)

	assert.NoError(t, ctlr.Start(ctx))
}

func TestControllerStartAlreadyStarted(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
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
	if err := ctlr.Start(ctx); err != exec.ErrTaskStarted {
		t.Fatalf("expected error %v, got %v", exec.ErrTaskPrepared, err)
	}
}

func TestControllerWait(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	evs, errs := makeEvents(t, config, "create", "die")
	gomock.InOrder(
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "running",
					},
				},
			}, nil),
		client.EXPECT().Events(gomock.Any(), types.EventsOptions{
			Since:   "0",
			Filters: config.eventFilter(),
		}).Return(evs, errs),
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "stopped", // can be anything but created
					},
				},
			}, nil),
	)

	assert.NoError(t, ctlr.Wait(ctx))
}

func TestControllerWaitUnhealthy(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)
	evs, errs := makeEvents(t, config, "create", "health_status: unhealthy")
	gomock.InOrder(
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "running",
					},
				},
			}, nil),
		client.EXPECT().Events(gomock.Any(), types.EventsOptions{
			Since:   "0",
			Filters: config.eventFilter(),
		}).Return(evs, errs),
		client.EXPECT().ContainerStop(gomock.Any(), config.name(), &tenSecond),
	)

	assert.Equal(t, ctlr.Wait(ctx), ErrContainerUnhealthy)
}

func TestControllerWaitExitError(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)
	evs, errs := makeEvents(t, config, "create", "die")
	gomock.InOrder(
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "running",
					},
				},
			}, nil),
		client.EXPECT().Events(gomock.Any(), types.EventsOptions{
			Since:   "0",
			Filters: config.eventFilter(),
		}).Return(evs, errs),
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID: "cid",
					State: &types.ContainerState{
						Status:   "exited", // can be anything but created
						ExitCode: 1,
						Pid:      1,
					},
				},
			}, nil),
	)

	err := ctlr.Wait(ctx)
	checkExitError(t, 1, err)
}

func checkExitError(t *testing.T, expectedCode int, err error) {
	ec, ok := err.(exec.ExitCoder)
	if !ok {
		t.Fatalf("expected an exit error, got: %v", err)
	}

	assert.Equal(t, expectedCode, ec.ExitCode())
}

func TestControllerWaitExitedClean(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					State: &types.ContainerState{
						Status: "exited",
					},
				},
			}, nil),
	)

	err := ctlr.Wait(ctx)
	assert.Nil(t, err)
}

func TestControllerWaitExitedError(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerInspect(gomock.Any(), config.name()).
			Return(types.ContainerJSON{
				ContainerJSONBase: &types.ContainerJSONBase{
					ID: "cid",
					State: &types.ContainerState{
						Status:   "exited",
						ExitCode: 1,
						Pid:      1,
					},
				},
			}, nil),
	)

	err := ctlr.Wait(ctx)
	checkExitError(t, 1, err)
}

func TestControllerShutdown(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerStop(gomock.Any(), config.name(), &tenSecond),
	)

	assert.NoError(t, ctlr.Shutdown(ctx))
}

func TestControllerTerminate(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	client.EXPECT().ContainerKill(gomock.Any(), config.name(), "")

	assert.NoError(t, ctlr.Terminate(ctx))
}

func TestControllerRemove(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().ContainerStop(gomock.Any(), config.name(), &tenSecond),
		client.EXPECT().ContainerRemove(gomock.Any(), config.name(), types.ContainerRemoveOptions{
			RemoveVolumes: true,
			Force:         true,
		}),
	)

	assert.NoError(t, ctlr.Remove(ctx))
}

func genTestControllerEnv(t *testing.T, task *api.Task) (context.Context, *MockAPIClient, exec.Controller, *containerConfig, func(t *testing.T)) {
	mocks := gomock.NewController(t)
	client := NewMockAPIClient(mocks)
	ctlr, err := newController(client, task, nil)
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
	return ctx, client, ctlr, config, func(t *testing.T) {
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
				Container: &api.ContainerSpec{
					Image:           reference,
					StopGracePeriod: gogotypes.DurationProto(10 * time.Second),
				},
			},
		},
	}
}

func makeEvents(t *testing.T, container *containerConfig, actions ...string) (<-chan events.Message, <-chan error) {
	evs := make(chan events.Message, len(actions))
	for _, action := range actions {
		evs <- events.Message{
			Type:   events.ContainerEventType,
			Action: action,
			Actor: events.Actor{
				// TODO(stevvooe): Resolve container id.
				Attributes: map[string]string{
					"name": container.name(),
				},
			},
		}
	}
	close(evs)

	return evs, nil
}
