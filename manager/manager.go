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
	"github.com/docker/swarm-v2/manager/drainer"
	"github.com/docker/swarm-v2/manager/orchestrator"
	"github.com/docker/swarm-v2/manager/scheduler"
	"github.com/docker/swarm-v2/manager/state/raft"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
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
	config *Config

	apiserver    api.ClusterServer
	caserver     *ca.Server
	dispatcher   *dispatcher.Dispatcher
	orchestrator *orchestrator.Orchestrator
	drainer      *drainer.Drainer
	scheduler    *scheduler.Scheduler
	allocator    *allocator.Allocator
	server       *grpc.Server
	raftNode     *raft.Node

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
		return nil, fmt.Errorf("can't create raft node: %v", err)
	}

	store := raftNode.MemoryStore()

	opts := []grpc.ServerOption{
		grpc.Creds(config.SecurityConfig.ServerTLSCreds)}
	localAPI := clusterapi.NewServer(store)
	proxyAPI, err := api.NewRaftProxyClusterServer(localAPI, raftNode)
	if err != nil {
		return nil, err
	}

	m := &Manager{
		config:       config,
		apiserver:    proxyAPI,
		caserver:     ca.NewServer(config.SecurityConfig),
		dispatcher:   dispatcher.New(store, dispatcherConfig),
		server:       grpc.NewServer(opts...),
		raftNode:     raftNode,
		leadershipCh: leadershipCh,
	}

	api.RegisterCAServer(m.server, m.caserver)
	api.RegisterManagerServer(m.server, m)
	api.RegisterClusterServer(m.server, m.apiserver)
	api.RegisterDispatcherServer(m.server, m.dispatcher)

	return m, nil
}

// Run starts all manager sub-systems and the gRPC server at the configured
// address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) Run(ctx context.Context) error {
	lis := m.config.Listener
	if lis == nil {
		l, err := net.Listen(m.config.ListenProto, m.config.ListenAddr)
		if err != nil {
			return err
		}
		lis = l
	}

	m.raftNode.Server = m.server

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
					m.scheduler = scheduler.New(store)
					m.drainer = drainer.New(store)

					// TODO(stevvooe): Allocate a context that can be used to
					// shutdown underlying manager processes when leadership is
					// lost.

					allocator, err := allocator.New(store)
					if err != nil {
						log.G(ctx).WithError(err).Error("failed to create allocator")
						// TODO(stevvooe): It doesn't seem correct here to fail
						// creating the allocator but then use it anyways.
					}
					m.allocator = allocator

					m.leaderLock.Unlock()

					// Start all sub-components in separate goroutines.
					// TODO(aluzzardi): This should have some kind of error handling so that
					// any component that goes down would bring the entire manager down.

					if m.allocator != nil {
						if err := m.allocator.Start(ctx); err != nil {
							log.G(ctx).WithError(err).Error("allocator exited with an error")
						}
					}

					go func() {
						if err := m.scheduler.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("scheduler exited with an error")
						}
					}()
					go func() {
						if err := m.orchestrator.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("orchestrator exited with an error")
						}
					}()
					go func() {
						if err := m.drainer.Run(ctx); err != nil {
							log.G(ctx).WithError(err).Error("drainer exited with an error")
						}
					}()
				} else if newState == raft.IsFollower {
					m.leaderLock.Lock()
					m.drainer.Stop()
					m.orchestrator.Stop()
					m.scheduler.Stop()

					if m.allocator != nil {
						m.allocator.Stop()
					}

					m.drainer = nil
					m.orchestrator = nil
					m.scheduler = nil
					m.allocator = nil
					m.leaderLock.Unlock()
				}
			case <-m.managerDone:
				return
			}
		}
	}()

	ctx = log.WithLogger(ctx, log.G(ctx).WithFields(
		logrus.Fields{
			"proto": lis.Addr().Network(),
			"addr":  lis.Addr().String()}))
	log.G(ctx).Info("listening")
	go m.raftNode.Run(ctx)

	raft.Register(m.server, m.raftNode)

	// Wait for raft to become available.
	// FIXME(aaronl): This should not be handled by sleeping.
	time.Sleep(time.Second)

	return m.server.Serve(lis)
}

// Stop stops the manager. It immediately closes all open connections and
// active RPCs as well as stopping the scheduler.
func (m *Manager) Stop() {
	m.leaderLock.Lock()
	if m.drainer != nil {
		m.drainer.Stop()
	}
	if m.orchestrator != nil {
		m.orchestrator.Stop()
	}
	if m.scheduler != nil {
		m.scheduler.Stop()
	}
	m.leaderLock.Unlock()

	m.raftNode.Shutdown()
	m.server.Stop()
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
