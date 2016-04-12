package container

import (
	"flag"
	"testing"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarm-v2/agent/exec"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

var (
	dockerTestAddr string
)

func init() {
	flag.StringVar(&dockerTestAddr, "test.docker.addr", "", "Set the address of the docker instance for testing")
}

// TestRunnerFlowIntegration simply runs the Runner flow against a docker
// instance to make sure we don't blow up.
//
// This is great for ad-hoc testing while doing development. We can add more
// verification but it solves the problem of not being able to run tasks
// without a swarm setup.
//
// Run with something like this:
//
// 	go test -run TestRunnerFlowIntegration -test.docker.addr unix:///var/run/docker.sock
//
func TestRunnerFlowIntegration(t *testing.T) {
	if dockerTestAddr == "" {
		t.Skip("specify docker address to run integration")
	}

	ctx := context.Background()
	client, err := engineapi.NewClient(dockerTestAddr, "", nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, client)

	task := &objectspb.Task{
		ID:     "dockerexec-integration-task-id",
		JobID:  "dockerexec-integration-job-id",
		NodeID: "dockerexec-integration-node-id",
		Spec: &specspb.TaskSpec{
			Runtime: &specspb.TaskSpec_Container{
				Container: &typespb.Container{
					Command: []string{"sh", "-c", "sleep 5"},
					Image: &typespb.Image{
						Reference: "alpine",
					},
				},
			},
		},
	}

	runner, err := NewRunner(client, task)
	assert.NoError(t, err)
	assert.NotNil(t, runner)
	assert.NoError(t, runner.Prepare(ctx))
	assert.NoError(t, runner.Start(ctx))
	assert.NoError(t, runner.Wait(ctx))
	assert.NoError(t, runner.Shutdown(ctx))
	assert.NoError(t, runner.Remove(ctx))
	assert.NoError(t, runner.Close())

	// NOTE(stevvooe): testify has no clue how to correctly do error equality.
	if err := runner.Close(); err != exec.ErrRunnerClosed {
		t.Fatalf("expected runner to be closed: %v", err)
	}
}
