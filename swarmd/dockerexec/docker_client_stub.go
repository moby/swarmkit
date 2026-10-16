package dockerexec

import (
	"context"
	"io"
	"runtime"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// StubAPIClient implements the client.APIClient interface, but allows
// you to specify the behavior of each of the methods.
type StubAPIClient struct {
	client.APIClient
	calls              map[string]int
	ContainerCreateFn  func(_ context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error)
	ContainerInspectFn func(_ context.Context, containerID string) (container.InspectResponse, error)
	ContainerKillFn    func(_ context.Context, containerID, signal string) error
	ContainerRemoveFn  func(_ context.Context, containerID string, options client.ContainerRemoveOptions) error
	ContainerStartFn   func(_ context.Context, containerID string, options client.ContainerStartOptions) error
	ContainerStopFn    func(_ context.Context, containerID string, options client.ContainerStopOptions) error
	ImagePullFn        func(_ context.Context, refStr string, options client.ImagePullOptions) (io.ReadCloser, error)
	EventsFn           func(_ context.Context, options client.EventsListOptions) client.EventsResult
}

// NewStubAPIClient returns an initialized StubAPIClient
func NewStubAPIClient() *StubAPIClient {
	return &StubAPIClient{
		calls: make(map[string]int),
	}
}

// If function A calls updateCountsForSelf,
// The callCount[A] value will be incremented
func (sa *StubAPIClient) called() {
	pc, _, _, ok := runtime.Caller(1)
	if !ok {
		panic("failed to update counts")
	}
	// longName looks like 'github.com/moby/swarmkit/agent/exec.(*StubController).Prepare:1'
	longName := runtime.FuncForPC(pc).Name()
	parts := strings.Split(longName, ".")
	tail := strings.Split(parts[len(parts)-1], ":")
	sa.calls[tail[0]]++
}

// ContainerCreate is part of the APIClient interface
func (sa *StubAPIClient) ContainerCreate(ctx context.Context, options client.ContainerCreateOptions) (client.ContainerCreateResult, error) {
	sa.called()
	return sa.ContainerCreateFn(ctx, options)
}

// ContainerInspect is part of the APIClient interface
func (sa *StubAPIClient) ContainerInspect(ctx context.Context, containerID string, _ client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	sa.called()
	c, err := sa.ContainerInspectFn(ctx, containerID)
	if err != nil {
		return client.ContainerInspectResult{}, err
	}
	return client.ContainerInspectResult{Container: c}, nil
}

// ContainerKill is part of the APIClient interface
func (sa *StubAPIClient) ContainerKill(ctx context.Context, containerID string, options client.ContainerKillOptions) (client.ContainerKillResult, error) {
	sa.called()
	return client.ContainerKillResult{}, sa.ContainerKillFn(ctx, containerID, options.Signal)
}

// ContainerRemove is part of the APIClient interface
func (sa *StubAPIClient) ContainerRemove(ctx context.Context, containerID string, options client.ContainerRemoveOptions) (client.ContainerRemoveResult, error) {
	sa.called()
	return client.ContainerRemoveResult{}, sa.ContainerRemoveFn(ctx, containerID, options)
}

// ContainerStart is part of the APIClient interface
func (sa *StubAPIClient) ContainerStart(ctx context.Context, containerID string, options client.ContainerStartOptions) (client.ContainerStartResult, error) {
	sa.called()
	return client.ContainerStartResult{}, sa.ContainerStartFn(ctx, containerID, options)
}

// ContainerStop is part of the APIClient interface
func (sa *StubAPIClient) ContainerStop(ctx context.Context, containerID string, options client.ContainerStopOptions) (client.ContainerStopResult, error) {
	sa.called()
	return client.ContainerStopResult{}, sa.ContainerStopFn(ctx, containerID, options)
}

type fakeStreamResult struct {
	io.ReadCloser
	client.ImagePullResponse
}

func (e fakeStreamResult) Read(p []byte) (int, error) { return e.ReadCloser.Read(p) }
func (e fakeStreamResult) Close() error               { return e.ReadCloser.Close() }

// ImagePull is part of the APIClient interface
func (sa *StubAPIClient) ImagePull(ctx context.Context, refStr string, options client.ImagePullOptions) (client.ImagePullResponse, error) {
	sa.called()
	res, err := sa.ImagePullFn(ctx, refStr, options)
	return fakeStreamResult{ReadCloser: res}, err
}

// Events is part of the APIClient interface
func (sa *StubAPIClient) Events(ctx context.Context, options client.EventsListOptions) client.EventsResult {
	sa.called()
	return sa.EventsFn(ctx, options)
}
