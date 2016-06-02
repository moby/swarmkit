package exec

import (
	"github.com/docker/swarm-v2/api"
	"golang.org/x/net/context"
)

// Executor provides controllers for tasks.
type Executor interface {
	// Describe returns the underlying node description.
	Describe(ctx context.Context) (*api.NodeDescription, error)

	// Configure uses the node object state to propogate node
	// state to the underlying executor.
	Configure(ctx context.Context, node *api.Node) error

	// Controller provides a controller for the given task.
	Controller(t *api.Task) (Controller, error)
}
