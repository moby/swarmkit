package agent

import "golang.org/x/net/context"

// Runner controls execution of a task.
//
// All methods should be idempotent and thread-safe.
type Runner interface {
	// Prepare the task for execution. This should ensure that all resources
	// are created such that a call to start should execute immediately.
	Prepare(ctx context.Context) error

	// Start the target and return when it has started successfully.
	Start(ctx context.Context) error

	// Wait blocks until the target has exited.
	Wait(ctx context.Context) error

	// Shutdown requests to exit the target gracefully.
	Shutdown(ctx context.Context) error

	// Terminate the target.
	Terminate(ctx context.Context) error

	// Remove all resources allocated by the runner.
	Remove(ctx context.Context) error

	// Close closes any ephemeral resources associated with runner instance.
	Close() error
}
