package container

import (
	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

type executor struct {
	// TODO(stevvooe): This type needs to become much more sophisticated. It
	// needs to handle reconnection, errors and authentication.
	client engineapi.APIClient
}

// NewExecutor returns an executor from the docker client.
func NewExecutor(client engineapi.APIClient) exec.Executor {
	return &executor{
		client: client,
	}
}

// Describe returns the underlying node description from the docker client.
func (e *executor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	info, err := e.client.Info(ctx)
	if err != nil {
		return nil, err
	}

	description := &api.NodeDescription{
		Hostname: info.Name,
		Platform: &api.Platform{
			Architecture: info.Architecture,
			OS:           info.OSType,
		},
		Resources: &api.Resources{
			CPU:    float64(info.NCPU),
			Memory: info.MemTotal,
		},
	}

	return description, nil
}

// Runner returns a docker container runner.
func (e *executor) Runner(t *api.Task) (exec.Runner, error) {
	runner, err := NewRunner(e.client, t)
	if err != nil {
		return nil, err
	}

	return runner, nil
}
