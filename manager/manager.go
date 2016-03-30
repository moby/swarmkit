package manager

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/clusterapi"
	"github.com/docker/swarm-v2/manager/collector"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/drainer"
	"github.com/docker/swarm-v2/manager/orchestrator"
	"github.com/docker/swarm-v2/manager/scheduler"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

var _ api.ManagerServer = &Manager{}

// Config is used to tune the Manager.
type Config struct {
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

	DispatcherConfig *dispatcher.Config
}

// Manager is the cluster manager for Swarm.
// This is the high-level object holding and initializing all the manager
// subsystems.
type Manager struct {
	config *Config

	apiserver    *clusterapi.Server
	dispatcher   *dispatcher.Dispatcher
	orchestrator *orchestrator.Orchestrator
	drainer      *drainer.Drainer
	scheduler    *scheduler.Scheduler
	server       *grpc.Server
	raftNode     *state.Node

	leadershipCh chan state.LeadershipState
	leaderLock   sync.Mutex

	managerDone chan struct{}
}

type clusterConns struct {
	cl *state.Cluster
}

func (cc *clusterConns) Connections() []*collector.ManagerConn {
	var res []*collector.ManagerConn
	for _, m := range cc.cl.Peers() {
		if m.Client == nil {
			continue
		}
		res = append(res, &collector.ManagerConn{Addr: m.Addr, Conn: m.Client.Conn})
	}
	return res
}

// New creates a Manager which has not started to accept requests yet.
func New(config *Config) (*Manager, error) {
	dispatcherConfig := dispatcher.DefaultConfig()

	if config.Listener != nil {
		config.ListenAddr = config.Listener.Addr().String()
	}
	// TODO(stevvooe): Reported address of manager is plumbed to listen addr
	// for now, may want to make this separate. This can be tricky to get right
	// so we need to make it easy to override. This needs to be the address
	// through which agent nodes access the manager.
	config.DispatcherConfig.Addr = config.ListenAddr

	err := os.MkdirAll(config.StateDir, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create state directory: %v", err)
	}

	ctx := context.Background()

	raftStateDir := filepath.Join(config.StateDir, "raft")
	err = os.MkdirAll(raftStateDir, 0700)
	if err != nil {
		return nil, fmt.Errorf("failed to create raft state directory: %v", err)
	}

	raftCfg := state.DefaultNodeConfig()

	leadershipCh := make(chan state.LeadershipState)

	newNodeOpts := state.NewNodeOptions{
		Addr:     config.ListenAddr,
		Config:   raftCfg,
		StateDir: raftStateDir,
	}
	raftNode, err := state.NewNode(ctx, newNodeOpts, leadershipCh)
	if err != nil {
		return nil, fmt.Errorf("can't create raft node: %v", err)
	}

	store := raftNode.MemoryStore()

	collector := collector.New(ctx, &clusterConns{cl: raftNode.Cluster}, collector.DefaultConfig())

	m := &Manager{
		config:       config,
		apiserver:    clusterapi.NewServer(store),
		dispatcher:   dispatcher.New(store, collector, dispatcherConfig),
		server:       grpc.NewServer(),
		raftNode:     raftNode,
		leadershipCh: leadershipCh,
	}

	api.RegisterManagerServer(m.server, m)
	api.RegisterClusterServer(m.server, m.apiserver)
	api.RegisterDispatcherServer(m.server, m.dispatcher)

	return m, nil
}

func (m *Manager) startRaftNode() {
	errCh := m.raftNode.Start()
	go func() {
		for {
			select {
			case err := <-errCh:
				log.Error(err)
			case <-m.managerDone:
				return
			}
		}
	}()
}

func (m *Manager) joinRaft() error {
	m.startRaftNode()

	c, err := state.GetRaftClient(m.config.JoinRaft, 10*time.Second)
	if err != nil {
		return fmt.Errorf("can't join raft cluster: %v", err)
	}

	resp, err := c.Join(m.raftNode.Ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: m.raftNode.Config.ID, Addr: m.config.ListenAddr},
	})
	if err != nil {
		return fmt.Errorf("can't join raft cluster: %v", err)
	}

	err = m.raftNode.RegisterNodes(resp.Members)
	if err != nil {
		return fmt.Errorf("can't add members to the local cluster list: %v", err)
	}

	return nil
}

// Run starts all manager sub-systems and the gRPC server at the configured
// address.
// The call never returns unless an error occurs or `Stop()` is called.
func (m *Manager) Run() error {
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
				if newState == state.IsLeader {
					store := m.raftNode.MemoryStore()

					m.leaderLock.Lock()
					m.orchestrator = orchestrator.New(store)
					m.scheduler = scheduler.New(store)
					m.drainer = drainer.New(store)
					m.leaderLock.Unlock()

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
					go func() {
						if err := m.drainer.Run(); err != nil {
							log.Error(err)
						}
					}()
				} else if newState == state.IsFollower {
					m.leaderLock.Lock()
					m.drainer.Stop()
					m.orchestrator.Stop()
					m.scheduler.Stop()

					m.drainer = nil
					m.orchestrator = nil
					m.scheduler = nil
					m.leaderLock.Unlock()
				}
			case <-m.managerDone:
				return
			}
		}
	}()

	log.WithFields(log.Fields{"proto": lis.Addr().Network(), "addr": lis.Addr().String()}).Info("Listening for connections")
	if m.config.JoinRaft != "" {
		if err := m.joinRaft(); err != nil {
			return err
		}
	} else {
		m.startRaftNode()
	}

	state.Register(m.server, m.raftNode)

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
	return &api.NodeCountResponse{
		Count: m.dispatcher.NodeCount(),
	}, nil
}
