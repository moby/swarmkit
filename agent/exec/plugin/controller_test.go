package plugin

import (
	"fmt"
	"runtime"
	"testing"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/log"
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
//+go:generate mockgen -package container -destination ../api_client_test.mock.go github.com/docker/docker/client APIClient

func TestControllerPrepare(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().PluginInstall(gomock.Any(), config.image(), gomock.Any()).
			Return(nil),
	)

	assert.NoError(t, ctlr.Prepare(ctx))
}

func TestControllerPrepareAlreadyPrepared(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().PluginInstall(gomock.Any(), config.image(), gomock.Any()).
			Return(fmt.Errorf("exists")),
		client.EXPECT().PluginInspectWithRaw(ctx, config.image()).
			Return(&types.Plugin{}, nil, nil),
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
		client.EXPECT().PluginInspectWithRaw(ctx, config.image()).
			Return(&types.Plugin{
				Enabled: false,
			}, nil, nil),
		client.EXPECT().PluginEnable(ctx, config.image()).
			Return(nil),
	)

	assert.NoError(t, ctlr.Start(ctx))
}

func TestControllerStartAlreadyStarted(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().PluginInspectWithRaw(ctx, config.image()).
			Return(&types.Plugin{
				Enabled: true,
			}, nil, nil),
	)

	// ensure idempotence
	if err := ctlr.Start(ctx); err != exec.ErrTaskStarted {
		t.Fatalf("expected error %v, got %v", exec.ErrTaskPrepared, err)
	}
}

func TestControllerShutdown(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	client.EXPECT().PluginDisable(gomock.Any(), config.image())

	assert.NoError(t, ctlr.Shutdown(ctx))
}

func TestControllerTerminate(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	client.EXPECT().PluginDisable(gomock.Any(), config.image())

	assert.NoError(t, ctlr.Terminate(ctx))
}

func TestControllerRemove(t *testing.T) {
	task := genTask(t)
	ctx, client, ctlr, config, finish := genTestControllerEnv(t, task)
	defer finish(t)

	gomock.InOrder(
		client.EXPECT().PluginDisable(gomock.Any(), config.image()),
		client.EXPECT().PluginRemove(gomock.Any(), config.image(), types.PluginRemoveOptions{}),
	)

	assert.NoError(t, ctlr.Remove(ctx))
}

func genTestControllerEnv(t *testing.T, task *api.Task) (context.Context, *exec.MockAPIClient, exec.Controller, *pluginConfig, func(t *testing.T)) {
	mocks := gomock.NewController(t)
	client := exec.NewMockAPIClient(mocks)
	ctlr, err := NewController(client, task)
	assert.NoError(t, err)

	config, err := newPluginConfig(task)
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
			Runtime: &api.TaskSpec_Plugin{
				Plugin: &api.PluginSpec{
					Image: reference,
				},
			},
		},
	}
}

/*
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
*/
