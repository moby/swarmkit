package container

import (
	"flag"
	"testing"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
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
//
func TestControllerFlowIntegration(t *testing.T) {
	if dockerTestAddr == "" {
		t.Skip("specify docker address to run integration")
	}

	ctx := context.Background()
	client, err := engineapi.NewClient(dockerTestAddr, "", nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, client)

	task := &api.Task{
		ID:        "dockerexec-integration-task-id",
		ServiceID: "dockerexec-integration-service-id",
		NodeID:    "dockerexec-integration-node-id",
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Command: []string{"sh", "-c", "sleep 5"},
					Image:   "alpine",
				},
			},
		},
	}

	ctlr, err := newController(client, task)
	assert.NoError(t, err)
	assert.NotNil(t, ctlr)
	assert.NoError(t, ctlr.Prepare(ctx))
	assert.NoError(t, ctlr.Start(ctx))
	assert.NoError(t, ctlr.Wait(ctx))
	assert.NoError(t, ctlr.Shutdown(ctx))
	assert.NoError(t, ctlr.Remove(ctx))
	assert.NoError(t, ctlr.Close())

	// NOTE(stevvooe): testify has no clue how to correctly do error equality.
	if err := ctlr.Close(); err != exec.ErrControllerClosed {
		t.Fatalf("expected controller to be closed: %v", err)
	}
}
