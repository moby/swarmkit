package exec

import (
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"golang.org/x/net/context"
)

// Executor provides runners (controllers) for tasks.
type Executor interface {
	// TODO(stevvooe): Allow instropsection of tasks known by executor.

	// Describe returns the underlying node description.
	Describe(ctx context.Context) (*typespb.NodeDescription, error)

	// Runner provides a runner for the given task.
	Runner(t *objectspb.Task) (Runner, error)
}
