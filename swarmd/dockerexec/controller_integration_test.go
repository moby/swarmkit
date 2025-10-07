package dockerexec

import (
	"context"
	"flag"
	"testing"

	engineapi "github.com/docker/docker/client"
	"github.com/moby/swarmkit/v2/agent/exec"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/api/genericresource"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	dockerTestAddr string
)

func init() {
	flag.StringVar(&dockerTestAddr, "test.docker.addr", "", "Set the address of the docker instance for testing")
}

// TestControllerFlowIntegration simply runs the Controller flow against a docker
// instance to make sure we don't blow up.
//
// This is great for ad-hoc testing while doing development. We can add more
// verification but it solves the problem of not being able to run tasks
// without a swarm setup.
//
// Run with something like this:
//
//	go test -run TestControllerFlowIntegration -test.docker.addr unix:///var/run/docker.sock
func TestControllerFlowIntegration(t *testing.T) {
	if dockerTestAddr == "" {
		t.Skip("specify docker address to run integration")
	}

	ctx := context.Background()
	client, err := engineapi.NewClientWithOpts(
		engineapi.WithHost(dockerTestAddr),
		engineapi.WithAPIVersionNegotiation(),
	)
	require.NoError(t, err)
	assert.NotNil(t, client)

	available := genericresource.NewSet("apple", "blue", "red")
	available = append(available, genericresource.NewDiscrete("orange", 3))

	task := &api.Task{
		ID:        "dockerexec-integration-task-id",
		ServiceID: "dockerexec-integration-service-id",
		NodeID:    "dockerexec-integration-node-id",
		ServiceAnnotations: api.Annotations{
			Name: "dockerexec-integration",
		},
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Command: []string{"sh", "-c", "sleep 5; echo $apple $orange; echo stderr >&2"},
					Image:   "alpine",
				},
			},
		},
		AssignedGenericResources: available,
	}

	var receivedLogs bool
	publisher := exec.LogPublisherFunc(func(ctx context.Context, message api.LogMessage) error {
		receivedLogs = true
		v1 := genericresource.Value(available[0])
		v2 := genericresource.Value(available[1])
		genericResourceString := v1 + " " + v2 + "\n"

		switch message.Stream {
		case api.LogStreamStdout:
			assert.Equal(t, genericResourceString, string(message.Data))
		case api.LogStreamStderr:
			assert.Equal(t, "stderr\n", string(message.Data))
		}

		t.Log(message)
		return nil
	})

	ctlr, err := newController(client, nil, task, nil)
	require.NoError(t, err)
	assert.NotNil(t, ctlr)
	require.NoError(t, ctlr.Prepare(ctx))
	require.NoError(t, ctlr.Start(ctx))
	require.NoError(t, ctlr.(exec.ControllerLogs).Logs(ctx, publisher, api.LogSubscriptionOptions{
		Follow: true,
	}))
	require.NoError(t, ctlr.Wait(ctx))
	assert.True(t, receivedLogs)
	require.NoError(t, ctlr.Shutdown(ctx))
	require.NoError(t, ctlr.Remove(ctx))
	require.NoError(t, ctlr.Close())

	// NOTE(stevvooe): testify has no clue how to correctly do error equality.
	err = ctlr.Close()
	require.Equalf(t, err, exec.ErrControllerClosed, "expected controller to be closed: %v", err)
}
