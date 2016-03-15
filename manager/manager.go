package manager

import (
	"net"

	"golang.org/x/net/context"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/clusterapi"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/orchestrator"
	"github.com/docker/swarm-v2/manager/scheduler"
	"github.com/docker/swarm-v2/manager/state"
	"google.golang.org/grpc"
)

var _ api.ManagerServer = &Manager{}

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

	apiserver    *clusterapi.Server
	dispatcher   *dispatcher.Dispatcher
	orchestrator *orchestrator.Orchestrator
	scheduler    *scheduler.Scheduler
	server       *grpc.Server
}

// New creates a Manager which has not started to accept requests yet.
func New(config *Config) *Manager {
	dispatcherConfig := dispatcher.DefaultConfig()

	// TODO(stevvooe): Reported address of manager is plumbed to listen addr
	// for now, may want to make this separate. This can be tricky to get right
	// so we need to make it easy to override. This needs to be the address
	// through which agent nodes access the manager.
	dispatcherConfig.Addr = config.ListenAddr

	m := &Manager{
		config:       config,
		apiserver:    clusterapi.NewServer(config.Store),
		dispatcher:   dispatcher.New(config.Store, dispatcherConfig),
		orchestrator: orchestrator.New(config.Store),
		scheduler:    scheduler.New(config.Store),
		server:       grpc.NewServer(),
	}

	api.RegisterManagerServer(m.server, m)
	api.RegisterClusterServer(m.server, m.apiserver)
	api.RegisterDispatcherServer(m.server, m.dispatcher)

	return m
}

// Run starts all manager sub-systems and the gRPC server at the configured
// address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) Run() error {
	lis, err := net.Listen(m.config.ListenProto, m.config.ListenAddr)
	if err != nil {
		return err
	}

	// Start all sub-components in separate goroutines.
	// TODO(aluzzardi): This should have some kind of error handling so that
	// any component that goes down would bring the entire manager down.
	go func() {
		if err := m.scheduler.Run(); err != nil {
			log.Error(err)
		}
	}()
	go func() {
		if err := m.orchestrator.Run(); err != nil {
			log.Error(err)
		}
	}()

	log.WithFields(log.Fields{"proto": m.config.ListenProto, "addr": m.config.ListenAddr}).Info("Listening for connections")

	return m.server.Serve(lis)
}

// Stop stops the manager. It immediately closes all open connections and
// active RPCs as well as stopping the scheduler.
func (m *Manager) Stop() {
	m.server.Stop()
	m.orchestrator.Stop()
	m.scheduler.Stop()
}

// GRPC Methods

// NodeCount returns number of nodes connected to particular manager.
// Supposed to be called only by cluster leader.
func (m *Manager) NodeCount(ctx context.Context, r *api.NodeCountRequest) (*api.NodeCountResponse, error) {
	return &api.NodeCountResponse{
		Count: m.dispatcher.NodeCount(),
	}, nil
}
