package exec

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// Executor provides runners (controllers) for tasks.
type Executor interface {
	// TODO(stevvooe): Allow instropsection of tasks known by executor.

	// Describe returns the underlying node description.
	Describe(ctx context.Context) (*api.NodeDescription, error)

	// Runner provides a runner for the given task.
	Runner(t *api.Task) (Runner, error)
}
