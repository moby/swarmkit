package testutils

import (
	"net"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

// TestExecutor is executor for integration tests
type TestExecutor struct {
	mu   sync.Mutex
	desc *api.NodeDescription
}

// Describe just returns empty NodeDescription.
func (e *TestExecutor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.desc == nil {
		return &api.NodeDescription{}, nil
	}
	return e.desc.Copy(), nil
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

// UpdateNodeDescription sets the node description on the test executor
func (e *TestExecutor) UpdateNodeDescription(newDesc *api.NodeDescription) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.desc = newDesc
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

// MockDispatcher is a fake dispatcher that one agent at a time can connect to
type MockDispatcher struct {
	mu             sync.Mutex
	sessionCh      chan *api.SessionMessage
	openSession    *api.SessionRequest
	closedSessions []*api.SessionRequest

	Addr string
}

// UpdateTaskStatus is not implemented
func (m *MockDispatcher) UpdateTaskStatus(context.Context, *api.UpdateTaskStatusRequest) (*api.UpdateTaskStatusResponse, error) {
	panic("not implemented")
}

// Tasks keeps an open stream until canceled
func (m *MockDispatcher) Tasks(_ *api.TasksRequest, stream api.Dispatcher_TasksServer) error {
	select {
	case <-stream.Context().Done():
	}
	return nil
}

// Assignments keeps an open stream until canceled
func (m *MockDispatcher) Assignments(_ *api.AssignmentsRequest, stream api.Dispatcher_AssignmentsServer) error {
	select {
	case <-stream.Context().Done():
	}
	return nil
}

// Heartbeat always successfully heartbeats
func (m *MockDispatcher) Heartbeat(context.Context, *api.HeartbeatRequest) (*api.HeartbeatResponse, error) {
	return &api.HeartbeatResponse{Period: time.Second * 5}, nil
}

// Session allows a session to be established, and sends the node info
func (m *MockDispatcher) Session(r *api.SessionRequest, stream api.Dispatcher_SessionServer) error {
	m.mu.Lock()
	m.openSession = r
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.closedSessions = append(m.closedSessions, m.openSession)
		m.openSession = nil
	}()

	// send the initial message first
	if err := stream.Send(&api.SessionMessage{
		SessionID: r.SessionID,
		Managers: []*api.WeightedPeer{
			{
				Peer: &api.Peer{Addr: m.Addr},
			},
		},
	}); err != nil {
		return err
	}

	ctx := stream.Context()
	for {
		select {
		case msg := <-m.sessionCh:
			msg.SessionID = r.SessionID
			if err := stream.Send(msg); err != nil {
				return err
			}
		case <-ctx.Done():
			return nil
		}
	}
}

// GetSessions return all the established and closed sessions
func (m *MockDispatcher) GetSessions() (*api.SessionRequest, []*api.SessionRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.openSession, m.closedSessions
}

// SessionMessageChannel returns a writable channel to inject session messages
func (m *MockDispatcher) SessionMessageChannel() chan<- *api.SessionMessage {
	return m.sessionCh
}

// NewMockDispatcher starts and returns a mock dispatcher instance that can be connected to
func NewMockDispatcher(t *testing.T, secConfig *ca.SecurityConfig) (*MockDispatcher, func()) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	addr := l.Addr().String()
	require.NoError(t, err)

	serverOpts := []grpc.ServerOption{grpc.Creds(secConfig.ServerTLSCreds)}
	s := grpc.NewServer(serverOpts...)

	m := &MockDispatcher{
		Addr:      addr,
		sessionCh: make(chan *api.SessionMessage, 1),
	}
	api.RegisterDispatcherServer(s, m)
	go s.Serve(l)
	return m, s.Stop
}
