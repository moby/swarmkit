package manager

import (
	"net"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/state"
	"google.golang.org/grpc"
)

// Config is used to tune the Manager.
type Config struct {
	Store state.Store

	ListenProto string
	ListenAddr  string
}

// Manager is the cluster manager for Swarm.
// This is the high-level object holding and initializing all the manager
// subsystems.
type Manager struct {
	config *Config

	dispatcher *dispatcher.Dispatcher
	server     *grpc.Server
}

// New creates a Manager which has not started to accept requests yet.
func New(config *Config) *Manager {
	m := &Manager{
		config:     config,
		dispatcher: dispatcher.New(config.Store, dispatcher.DefaultConfig()),
	}

	m.server = grpc.NewServer()
	api.RegisterAgentServer(m.server, m.dispatcher)

	return m
}

// ListenAndServe starts a gRPC server with the configured address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) ListenAndServe() error {
	lis, err := net.Listen(m.config.ListenProto, m.config.ListenAddr)
	if err != nil {
		return err
	}
	return m.server.Serve(lis)
}

// Stop stops the manager. It immediately closes all open connections and
// active RPCs.
func (m *Manager) Stop() {
	m.server.Stop()
}
