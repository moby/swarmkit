package exec

import "github.com/docker/swarm-v2/api"

// Executor provides runners (controllers) for tasks.
type Executor interface {
	// TODO(stevvooe): Allow instropsection of tasks known by executor.

	// Runner provides a runner for the given task.
	Runner(t *api.Task) (Runner, error)
}
