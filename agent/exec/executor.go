package exec

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// Executor provides controllers for tasks.
type Executor interface {
	// TODO(stevvooe): Allow instropsection of tasks known by executor.

	// Describe returns the underlying node description.
	Describe(ctx context.Context) (*api.NodeDescription, error)

	// Controller provides a controller for the given task.
	Controller(t *api.Task) (Controller, error)
}
