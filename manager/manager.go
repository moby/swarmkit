package manager

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/allocator"
	"github.com/docker/swarm-v2/manager/clusterapi"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/orchestrator"
	"github.com/docker/swarm-v2/manager/raftpicker"
	"github.com/docker/swarm-v2/manager/scheduler"
	"github.com/docker/swarm-v2/manager/state/raft"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	// defaultTaskHistory is the number of tasks to keep.
	defaultTaskHistory = 10
)

var _ api.ManagerServer = &Manager{}

// Config is used to tune the Manager.
type Config struct {
	SecurityConfig *ca.ManagerSecurityConfig

	ListenProto string
	ListenAddr  string
	// Listener will be used for grpc serving if it's not nil, ListenProto and
	// ListenAddr fields will be used otherwise.
	Listener net.Listener

	// JoinRaft is an optional address of a node in an existing raft
	// cluster to join.
	JoinRaft string

	// Top-level state directory
	StateDir string
}

// Manager is the cluster manager for Swarm.
// This is the high-level object holding and initializing all the manager
// subsystems.
type Manager struct {
	config   *Config
	listener net.Listener

	caserver         *ca.Server
	dispatcher       *dispatcher.Dispatcher
	orchestrator     *orchestrator.Orchestrator
	fillOrchestrator *orchestrator.FillOrchestrator
	taskReaper       *orchestrator.TaskReaper
	scheduler        *scheduler.Scheduler
	allocator        *allocator.Allocator
	server           *grpc.Server
	raftNode         *raft.Node

	leadershipCh chan raft.LeadershipState
	leaderLock   sync.Mutex

	managerDone chan struct{}
}

// New creates a Manager which has not started to accept requests yet.
func New(config *Config) (*Manager, error) {
	dispatcherConfig := dispatcher.DefaultConfig()

	// TODO(stevvooe): Reported address of manager is plumbed to listen addr
	// for now, may want to make this separate. This can be tricky to get right
	// so we need to make it easy to override. This needs to be the address
	// through which agent nodes access the manager.
	dispatcherConfig.Addr = config.ListenAddr

	err := os.MkdirAll(config.StateDir, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create state directory: %v", err)
	}

	raftStateDir := filepath.Join(config.StateDir, "raft")
	err = os.MkdirAll(raftStateDir, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft state directory: %v", err)
	}

	lis := config.Listener
	if lis == nil {
		l, err := net.Listen(config.ListenProto, config.ListenAddr)
		if err != nil {
			return nil, err
		}
		lis = l
	}

	raftCfg := raft.DefaultNodeConfig()

	leadershipCh := make(chan raft.LeadershipState)

	newNodeOpts := raft.NewNodeOptions{
		Addr:           config.ListenAddr,
		JoinAddr:       config.JoinRaft,
		Config:         raftCfg,
		StateDir:       raftStateDir,
		TLSCredentials: config.SecurityConfig.ClientTLSCreds,
	}
	raftNode, err := raft.NewNode(context.TODO(), newNodeOpts, leadershipCh)
	if err != nil {
		lis.Close()
		return nil, fmt.Errorf("can't create raft node: %v", err)
	}

	store := raftNode.MemoryStore()

	opts := []grpc.ServerOption{
		grpc.Creds(config.SecurityConfig.ServerTLSCreds)}

	m := &Manager{
		config:       config,
		listener:     lis,
		caserver:     ca.NewServer(config.SecurityConfig),
		dispatcher:   dispatcher.New(store, dispatcherConfig),
		server:       grpc.NewServer(opts...),
		raftNode:     raftNode,
		leadershipCh: leadershipCh,
	}

	return m, nil
}

// Run starts all manager sub-systems and the gRPC server at the configured
// address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) Run(ctx context.Context) error {
	m.managerDone = make(chan struct{})
	defer close(m.managerDone)

	go func() {
		for {
			select {
			case newState := <-m.leadershipCh:
				if newState == raft.IsLeader {
					store := m.raftNode.MemoryStore()

					m.leaderLock.Lock()
					m.orchestrator = orchestrator.New(store)
					m.fillOrchestrator = orchestrator.NewFillOrchestrator(store)
					m.taskReaper = orchestrator.NewTaskReaper(store, defaultTaskHistory)
					m.scheduler = scheduler.New(store)

					// TODO(stevvooe): Allocate a context that can be used to
					// shutdown underlying manager processes when leadership is
					// lost.

					var err error
					m.allocator, err = allocator.New(store)
					if err != nil {
						log.G(ctx).WithError(err).Error("failed to create allocator")
						// TODO(stevvooe): It doesn't seem correct here to fail
						// creating the allocator but then use it anyways.
					}

					// Start all sub-components in separate goroutines.
					// TODO(aluzzardi): This should have some kind of error handling so that
					// any component that goes down would bring the entire manager down.

					if m.allocator != nil {
						go func(allocator *allocator.Allocator) {
							if err := allocator.Run(ctx); err != nil {
								log.G(ctx).WithError(err).Error("allocator exited with an error")
							}
						}(m.allocator)
					}

					go func(scheduler *scheduler.Scheduler) {
						if err := scheduler.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("scheduler exited with an error")
						}
					}(m.scheduler)
					go func(taskReaper *orchestrator.TaskReaper) {
						taskReaper.Run()
					}(m.taskReaper)
					go func(orchestrator *orchestrator.Orchestrator) {
						if err := orchestrator.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("orchestrator exited with an error")
						}
					}(m.orchestrator)
					go func(fillOrchestrator *orchestrator.FillOrchestrator) {
						if err := fillOrchestrator.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("fillOrchestrator exited with an error")
						}
					}(m.fillOrchestrator)

					m.leaderLock.Unlock()
				} else if newState == raft.IsFollower {
					m.leaderLock.Lock()

					if m.allocator != nil {
						m.allocator.Stop()
						m.allocator = nil
					}

					m.orchestrator.Stop()
					m.orchestrator = nil

					m.fillOrchestrator.Stop()
					m.fillOrchestrator = nil

					m.taskReaper.Stop()
					m.taskReaper = nil

					m.scheduler.Stop()
					m.scheduler = nil

					m.leaderLock.Unlock()
				}
			case <-m.managerDone:
				return
			}
		}
	}()

	ctx = log.WithLogger(ctx, log.G(ctx).WithFields(
		logrus.Fields{
			"proto": m.listener.Addr().Network(),
			"addr":  m.listener.Addr().String()}))
	log.G(ctx).Info("listening")

	go func() {
		err := m.raftNode.Run(ctx)
		if err != nil {
			log.G(ctx).Error(err)
			m.Stop()
			os.Exit(1)
		}
	}()

	backoffConfig := *grpc.DefaultBackoffConfig
	backoffConfig.MaxDelay = 2 * time.Second

	proxyOpts := []grpc.DialOption{
		grpc.WithBackoffConfig(&backoffConfig),
		grpc.WithTransportCredentials(m.config.SecurityConfig.ClientTLSCreds),
	}

	cs := raftpicker.NewConnSelector(m.raftNode, proxyOpts...)

	localAPI := clusterapi.NewServer(m.raftNode.MemoryStore(), m.raftNode)
	proxyAPI := api.NewRaftProxyClusterServer(localAPI, cs, m.raftNode)

	api.RegisterCAServer(m.server, m.caserver)
	api.RegisterRaftServer(m.server, m.raftNode)
	api.RegisterManagerServer(m.server, m)
	api.RegisterClusterServer(m.server, proxyAPI)
	api.RegisterDispatcherServer(m.server, m.dispatcher)

	errServe := make(chan error, 1)
	go func() {
		errServe <- m.server.Serve(m.listener)
	}()

	leaderCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	if err := raft.WaitForLeader(leaderCtx, m.raftNode); err != nil {
		m.server.Stop()
		return err
	}
	cancel()
	return <-errServe
}

// Stop stops the manager. It immediately closes all open connections and
// active RPCs as well as stopping the scheduler.
func (m *Manager) Stop() {
	m.leaderLock.Lock()
	defer m.leaderLock.Unlock()
	if m.allocator != nil {
		m.allocator.Stop()
		m.allocator = nil
	}
	if m.orchestrator != nil {
		m.orchestrator.Stop()
		m.orchestrator = nil
	}
	if m.fillOrchestrator != nil {
		m.fillOrchestrator.Stop()
		m.fillOrchestrator = nil
	}
	if m.taskReaper != nil {
		m.taskReaper.Stop()
	}
	if m.scheduler != nil {
		m.scheduler.Stop()
		m.scheduler = nil
	}
	if m.raftNode != nil {
		m.raftNode.Shutdown()
		m.raftNode = nil
	}
	if m.server != nil {
		m.server.Stop()
		m.server = nil
	}
}

// GRPC Methods

// NodeCount returns number of nodes connected to particular manager.
// Supposed to be called only by cluster leader.
func (m *Manager) NodeCount(ctx context.Context, r *api.NodeCountRequest) (*api.NodeCountResponse, error) {
	managerID, err := ca.AuthorizeRole(ctx, []string{ca.ManagerRole})
	if err != nil {
		return nil, err
	}

	log.G(ctx).WithField("request", r).Debugf("(*Manager).NodeCount from node %s", managerID)

	return &api.NodeCountResponse{
		Count: m.dispatcher.NodeCount(),
	}, nil
}
