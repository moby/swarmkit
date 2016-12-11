package integration

import (
	"sync"

	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"golang.org/x/net/context"
)

// TestExecutor is executor for integration tests
type TestExecutor struct {
}

// Describe just returns empty NodeDescription.
func (e *TestExecutor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	return &api.NodeDescription{}, nil
}

// Configure does nothing.
func (e *TestExecutor) Configure(ctx context.Context, node *api.Node) error {
	return nil
}

// SetNetworkBootstrapKeys does nothing.
func (e *TestExecutor) SetNetworkBootstrapKeys([]*api.EncryptionKey) error {
	return nil
}

// Controller returns TestController.
func (e *TestExecutor) Controller(t *api.Task) (exec.Controller, error) {
	return &TestController{
		ch: make(chan struct{}),
	}, nil
}

// TestController is dummy channel based controller for tests.
type TestController struct {
	ch        chan struct{}
	closeOnce sync.Once
}

// Update does nothing.
func (t *TestController) Update(ctx context.Context, task *api.Task) error {
	return nil
}

// Prepare does nothing.
func (t *TestController) Prepare(ctx context.Context) error {
	return nil
}

// Start does nothing.
func (t *TestController) Start(ctx context.Context) error {
	return nil
}

// Wait waits on internal channel.
func (t *TestController) Wait(ctx context.Context) error {
	select {
	case <-t.ch:
	case <-ctx.Done():
	}
	return nil
}

// Shutdown closes internal channel
func (t *TestController) Shutdown(ctx context.Context) error {
	t.closeOnce.Do(func() {
		close(t.ch)
	})
	return nil
}

// Terminate closes internal channel if it wasn't closed before.
func (t *TestController) Terminate(ctx context.Context) error {
	t.closeOnce.Do(func() {
		close(t.ch)
	})
	return nil
}

// Remove does nothing.
func (t *TestController) Remove(ctx context.Context) error {
	return nil
}

// Close does nothing.
func (t *TestController) Close() error {
	t.closeOnce.Do(func() {
		close(t.ch)
	})
	return nil
}
