package manager

import (
	"net"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/clusterapi"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/scheduler"
	"github.com/docker/swarm-v2/state"
	"google.golang.org/grpc"
)

// Config is used to tune the Manager.
type Config struct {
	Store state.WatchableStore

	ListenProto string
	ListenAddr  string
}

// Manager is the cluster manager for Swarm.
// This is the high-level object holding and initializing all the manager
// subsystems.
type Manager struct {
	config *Config

	apiserver  *clusterapi.Server
	scheduler  *scheduler.Scheduler
	dispatcher *dispatcher.Dispatcher
	server     *grpc.Server
}

// New creates a Manager which has not started to accept requests yet.
func New(config *Config) *Manager {
	m := &Manager{
		config:     config,
		apiserver:  clusterapi.NewServer(config.Store),
		scheduler:  scheduler.New(config.Store),
		dispatcher: dispatcher.New(config.Store, dispatcher.DefaultConfig()),
		server:     grpc.NewServer(),
	}

	api.RegisterClusterServer(m.server, m.apiserver)
	api.RegisterDispatcherServer(m.server, m.dispatcher)

	return m
}

// ListenAndServe starts the scheduler and the gRPC server at the configured
// address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) ListenAndServe() error {
	if err := m.scheduler.Start(); err != nil {
		return err
	}

	lis, err := net.Listen(m.config.ListenProto, m.config.ListenAddr)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{"proto": m.config.ListenProto, "addr": m.config.ListenAddr}).Info("Listening for connections")
	return m.server.Serve(lis)
}

// Stop stops the manager. It immediately closes all open connections and
// active RPCs as well as stopping the scheduler.
func (m *Manager) Stop() {
	if err := m.scheduler.Stop(); err != nil {
		log.Errorf("Unable to stop the scheduler: %v", err)
	}
	m.server.Stop()
}
