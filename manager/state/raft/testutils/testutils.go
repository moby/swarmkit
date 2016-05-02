package testutils

import (
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"

	etcdraft "github.com/coreos/etcd/raft"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/raft"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNode represents a raft test node
type TestNode struct {
	*raft.Node
	Listener *wrappedListener
}

// AdvanceTicks advances the raft state machine fake clock
func AdvanceTicks(clockSource *fakeclock.FakeClock, ticks int) {
	// A FakeClock timer won't fire multiple times if time is advanced
	// more than its interval.
	for i := 0; i != ticks; i++ {
		clockSource.Increment(time.Second)
	}
}

// PollFunc is used to periodically execute a check function
func PollFunc(f func() error) error {
	if f() == nil {
		return nil
	}
	timeout := time.After(10 * time.Second)
	for {
		err := f()
		if err == nil {
			return nil
		}
		select {
		case <-timeout:
			return fmt.Errorf("polling failed: %v", err)
		case <-time.After(50 * time.Millisecond):
		}
	}
}

// WaitForCluster waits until leader will be one of specified nodes
func WaitForCluster(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*TestNode) {
	err := PollFunc(func() error {
		clockSource.Increment(time.Second)
		var prev *etcdraft.Status
	nodeLoop:
		for _, n := range nodes {
			if prev == nil {
				prev = new(etcdraft.Status)
				*prev = n.Status()
				for _, n2 := range nodes {
					if n2.Config.ID == prev.Lead {
						continue nodeLoop
					}
				}
				return errors.New("did not find leader in member list")
			}
			cur := n.Status()

			for _, n2 := range nodes {
				if n2.Config.ID == cur.Lead {
					if cur.Lead != prev.Lead || cur.Term != prev.Term || cur.Applied != prev.Applied {
						return errors.New("state does not match on all nodes")
					}
					continue nodeLoop
				}
			}
			return errors.New("did not find leader in member list")
		}
		return nil
	})
	require.NoError(t, err)
}

// WaitForPeerNumber waits until peers in cluster converge to specified number
func WaitForPeerNumber(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*TestNode, count int) {
	assert.NoError(t, PollFunc(func() error {
		clockSource.Increment(time.Second)
		for _, n := range nodes {
			if len(n.GetMemberlist()) != count {
				return errors.New("unexpected number of members")
			}
		}
		return nil
	}))
}

// wrappedListener disables the Close method to make it possible to reuse a
// socket. close must be called to release the socket.
type wrappedListener struct {
	net.Listener
	acceptConn chan net.Conn
	acceptErr  chan error
	closed     chan struct{}
}

func newWrappedListener(l net.Listener) *wrappedListener {
	wrappedListener := wrappedListener{
		Listener:   l,
		acceptConn: make(chan net.Conn),
		acceptErr:  make(chan error, 1),
		closed:     make(chan struct{}, 10), // grpc closes multiple times
	}
	// Accept connections
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				wrappedListener.acceptErr <- err
				return
			}
			wrappedListener.acceptConn <- conn
		}
	}()

	return &wrappedListener
}

func (l *wrappedListener) Accept() (net.Conn, error) {
	// closure must take precendence over taking a connection
	// from the channel
	select {
	case <-l.closed:
		return nil, errors.New("listener closed")
	default:
	}

	select {
	case conn := <-l.acceptConn:
		return conn, nil
	case err := <-l.acceptErr:
		return nil, err
	case <-l.closed:
		return nil, errors.New("listener closed")
	}
}

func (l *wrappedListener) Close() error {
	l.closed <- struct{}{}
	return nil
}

func (l *wrappedListener) close() error {
	return l.Listener.Close()
}

// recycleWrappedListener creates a new wrappedListener that uses the same
// listening socket as the supplied wrappedListener.
func recycleWrappedListener(old *wrappedListener) *wrappedListener {
	return &wrappedListener{
		Listener:   old.Listener,
		acceptConn: old.acceptConn,
		acceptErr:  old.acceptErr,
		closed:     make(chan struct{}, 10), // grpc closes multiple times
	}
}

// NewNode creates a new raft node to use for tests
func NewNode(t *testing.T, clockSource *fakeclock.FakeClock, securityConfig *ca.ManagerSecurityConfig, opts ...raft.NewNodeOptions) *TestNode {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "can't bind to raft service port")
	wrappedListener := newWrappedListener(l)

	serverOpts := []grpc.ServerOption{grpc.Creds(securityConfig.ServerTLSCreds)}
	s := grpc.NewServer(serverOpts...)

	cfg := raft.DefaultNodeConfig()

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := raft.NewNodeOptions{
		Addr:           l.Addr().String(),
		Config:         cfg,
		StateDir:       stateDir,
		ClockSource:    clockSource,
		SendTimeout:    10 * time.Second,
		TLSCredentials: securityConfig.ClientTLSCreds,
	}

	if len(opts) > 1 {
		panic("more than one optional argument provided")
	}
	if len(opts) == 1 {
		if opts[0].SnapshotInterval != 0 {
			newNodeOpts.SnapshotInterval = opts[0].SnapshotInterval
		}
		newNodeOpts.LogEntriesForSlowFollowers = opts[0].LogEntriesForSlowFollowers
		newNodeOpts.JoinAddr = opts[0].JoinAddr
	}

	n, err := raft.NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	return &TestNode{Node: n, Listener: wrappedListener}
}

// NewInitNode creates a new raft node initiating the cluster
// for other members to join
func NewInitNode(t *testing.T, securityConfig *ca.ManagerSecurityConfig, opts ...raft.NewNodeOptions) (*TestNode, *fakeclock.FakeClock) {
	ctx := context.Background()
	clockSource := fakeclock.NewFakeClock(time.Now())
	n := NewNode(t, clockSource, securityConfig, opts...)

	go n.Run(ctx)

	raft.Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.Listener))
	}()

	return n, clockSource
}

// NewJoinNode creates a new raft node joining an existing cluster
func NewJoinNode(t *testing.T, clockSource *fakeclock.FakeClock, join string, securityConfig *ca.ManagerSecurityConfig, opts ...raft.NewNodeOptions) *TestNode {
	var derivedOpts raft.NewNodeOptions
	if len(opts) == 1 {
		derivedOpts = opts[0]
	}
	derivedOpts.JoinAddr = join
	n := NewNode(t, clockSource, securityConfig, derivedOpts)

	go n.Run(context.Background())
	raft.Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.Listener))
	}()
	return n
}

// RestartNode restarts a raft test node
func RestartNode(t *testing.T, clockSource *fakeclock.FakeClock, oldNode *TestNode, securityConfig *ca.ManagerSecurityConfig) *TestNode {
	wrappedListener := recycleWrappedListener(oldNode.Listener)
	serverOpts := []grpc.ServerOption{grpc.Creds(securityConfig.ServerTLSCreds)}
	s := grpc.NewServer(serverOpts...)

	cfg := raft.DefaultNodeConfig()

	newNodeOpts := raft.NewNodeOptions{
		Addr:           oldNode.Address,
		Config:         cfg,
		StateDir:       oldNode.StateDir,
		ClockSource:    clockSource,
		SendTimeout:    10 * time.Second,
		TLSCredentials: securityConfig.ClientTLSCreds,
	}

	ctx := context.Background()
	n, err := raft.NewNode(ctx, newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	go n.Run(ctx)

	raft.Register(s, n)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, s.Serve(wrappedListener))
	}()
	return &TestNode{Node: n, Listener: wrappedListener}
}

// NewRaftCluster creates a new raft cluster with 3 nodes for testing
func NewRaftCluster(t *testing.T, securityConfig *ca.ManagerSecurityConfig, opts ...raft.NewNodeOptions) (map[uint64]*TestNode, *fakeclock.FakeClock) {
	var clockSource *fakeclock.FakeClock
	nodes := make(map[uint64]*TestNode)
	nodes[1], clockSource = NewInitNode(t, securityConfig, opts...)
	AddRaftNode(t, clockSource, nodes, securityConfig, opts...)
	AddRaftNode(t, clockSource, nodes, securityConfig, opts...)
	return nodes, clockSource
}

// AddRaftNode adds an additional raft test node to an existing cluster
func AddRaftNode(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*TestNode, securityConfig *ca.ManagerSecurityConfig, opts ...raft.NewNodeOptions) {
	n := uint64(len(nodes) + 1)
	nodes[n] = NewJoinNode(t, clockSource, nodes[1].Address, securityConfig, opts...)
	WaitForCluster(t, clockSource, nodes)
}

// TeardownCluster destroys a raft cluster used for tests
func TeardownCluster(t *testing.T, nodes map[uint64]*TestNode) {
	for _, node := range nodes {
		ShutdownNode(node)
		node.Listener.close()
	}
}

// ShutdownNode shuts down a raft test node and deletes the content
// of the state directory
func ShutdownNode(node *TestNode) {
	node.Server.Stop()
	node.Shutdown()
	os.RemoveAll(node.StateDir)
}

// Leader determines who is the leader amongst a set of raft nodes
// belonging to the same cluster
func Leader(nodes map[uint64]*TestNode) *TestNode {
	for _, n := range nodes {
		if n.Config.ID == n.Leader() {
			return n
		}
	}
	panic("could not find a leader")
}

// ProposeValue proposes a value to a raft test cluster
func ProposeValue(t *testing.T, raftNode *TestNode, nodeID ...string) (*api.Node, error) {
	nodeIDStr := "id1"
	if len(nodeID) != 0 {
		nodeIDStr = nodeID[0]
	}
	node := &api.Node{
		ID: nodeIDStr,
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: nodeIDStr,
			},
		},
	}

	storeActions := []*api.StoreAction{
		{
			Action: api.StoreActionKindCreate,
			Target: &api.StoreAction_Node{
				Node: node,
			},
		},
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second)

	err := raftNode.ProposeValue(ctx, storeActions, func() {
		err := raftNode.MemoryStore().ApplyStoreActions(storeActions)
		assert.NoError(t, err, "error applying actions")
	})
	if err != nil {
		return nil, err
	}

	return node, nil
}

// CheckValue checks that the value has been propagated between raft members
func CheckValue(t *testing.T, raftNode *TestNode, createdNode *api.Node) {
	assert.NoError(t, PollFunc(func() error {
		var err error
		raftNode.MemoryStore().View(func(tx state.ReadTx) {
			var allNodes []*api.Node
			allNodes, err = tx.Nodes().Find(state.All)
			if err != nil {
				return
			}
			if len(allNodes) != 1 {
				err = fmt.Errorf("expected 1 node, got %d nodes", len(allNodes))
				return
			}
			if !reflect.DeepEqual(allNodes[0], createdNode) {
				err = errors.New("node did not match expected value")
			}
		})
		return err
	}))
}

// CheckNoValue checks that there is no value replicated on nodes, generally
// used to test the absence of a leader
func CheckNoValue(t *testing.T, raftNode *TestNode) {
	assert.NoError(t, PollFunc(func() error {
		var err error
		raftNode.MemoryStore().View(func(tx state.ReadTx) {
			var allNodes []*api.Node
			allNodes, err = tx.Nodes().Find(state.All)
			if err != nil {
				return
			}
			if len(allNodes) != 0 {
				err = fmt.Errorf("expected no nodes, got %d", len(allNodes))
			}
		})
		return err
	}))
}

// CheckValuesOnNodes checks that all the nodes in the cluster have the same
// replicated data, generally used to check if a node can catch up with the logs
// correctly
func CheckValuesOnNodes(t *testing.T, checkNodes map[uint64]*TestNode, ids []string, values []*api.Node) {
	for _, node := range checkNodes {
		assert.NoError(t, PollFunc(func() error {
			var err error
			node.MemoryStore().View(func(tx state.ReadTx) {
				var allNodes []*api.Node
				allNodes, err = tx.Nodes().Find(state.All)
				if err != nil {
					return
				}
				if len(allNodes) != len(ids) {
					err = fmt.Errorf("expected %d nodes, got %d", len(ids), len(allNodes))
					return
				}

				for i, id := range ids {
					n := tx.Nodes().Get(id)
					if !reflect.DeepEqual(values[i], n) {
						err = fmt.Errorf("node %s did not match expected value", id)
						return
					}
				}
			})
			return err
		}))
	}
}
