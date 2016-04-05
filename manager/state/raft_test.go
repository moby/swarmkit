package state

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/coreos/etcd/raft"
	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTick = 1 * time.Second

var (
	raftLogger = &raft.DefaultLogger{Logger: log.New(ioutil.Discard, "", 0)}
)

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
}

type testNode struct {
	*Node
	listener *wrappedListener
}

func pollFunc(f func() bool) error {
	if f() {
		return nil
	}
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	done := make(chan struct{})
	go func() {
		for range tick.C {
			if f() {
				break
			}
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(32 * time.Second):
		return fmt.Errorf("polling failed")
	}
}

type leaderStatus struct {
	leader uint64
	term   uint64
}

func getTermAndLeader(n *testNode) *leaderStatus {
	status := n.Status()
	return &leaderStatus{leader: status.Lead, term: status.Term}
}

// waitForCluster waits until leader will be one of specified nodes
func waitForCluster(t *testing.T, nodes map[uint64]*testNode) {
	err := pollFunc(func() bool {
		var prev *leaderStatus
	nodeLoop:
		for _, n := range nodes {
			if prev == nil {
				prev = getTermAndLeader(n)
				for _, n2 := range nodes {
					if n2.Config.ID == prev.leader {
						continue nodeLoop
					}
				}
				return false
			}
			cur := getTermAndLeader(n)

			for _, n2 := range nodes {
				if n2.Config.ID == cur.leader {
					if cur.leader != prev.leader || cur.term != prev.term {
						return false
					}
					continue nodeLoop
				}
			}
			return false
		}
		return true
	})
	require.NoError(t, err)
}

// waitForPeerNumber waits until peers in cluster converge to specified number
func waitForPeerNumber(t *testing.T, nodes map[uint64]*testNode, count int) {
	assert.NoError(t, pollFunc(func() bool {
		for _, n := range nodes {
			if len(n.cluster.Members()) != count {
				return false
			}
		}
		return true
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

func (l *wrappedListener) Addr() net.Addr {
	// stubbed out
	return l.Listener.Addr()
}

func newNode(t *testing.T, opts ...NewNodeOptions) *testNode {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "can't bind to raft service port")
	wrappedListener := newWrappedListener(l)
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		Addr:         l.Addr().String(),
		Config:       cfg,
		StateDir:     stateDir,
		TickInterval: testTick,
	}

	if len(opts) > 1 {
		panic("more than one optional argument provided")
	}
	if len(opts) == 1 {
		if opts[0].SnapshotInterval != 0 {
			newNodeOpts.SnapshotInterval = opts[0].SnapshotInterval
		}
		newNodeOpts.LogEntriesForSlowFollowers = opts[0].LogEntriesForSlowFollowers
	}

	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	return &testNode{Node: n, listener: wrappedListener}
}

func newInitNode(t *testing.T, opts ...NewNodeOptions) *testNode {
	n := newNode(t, opts...)

	err := n.Campaign(n.Ctx)
	require.NoError(t, err, "can't campaign to be the leader")
	n.Start()

	Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()

	return n
}

func newJoinNode(t *testing.T, join string, opts ...NewNodeOptions) *testNode {
	n := newNode(t, opts...)

	n.Start()

	c, err := GetRaftClient(join, 500*time.Millisecond)
	assert.NoError(t, err, "can't initiate connection with existing raft")

	ctx, _ := context.WithTimeout(context.Background(), 1000*time.Millisecond)
	resp, err := c.Join(ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: n.Config.ID, Addr: n.Address},
	})
	require.NoError(t, err, "can't join existing Raft")

	n.RegisterNodes(resp.Members)

	Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()
	return n
}

func restartNode(t *testing.T, oldNode *testNode, join string) *testNode {
	wrappedListener := recycleWrappedListener(oldNode.listener)
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	newNodeOpts := NewNodeOptions{
		Addr:         oldNode.Address,
		Config:       cfg,
		StateDir:     oldNode.stateDir,
		TickInterval: testTick,
	}

	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	n.Start()

	c, err := GetRaftClient(join, 500*time.Millisecond)
	assert.NoError(t, err, "can't initiate connection with existing raft")

	resp, err := c.Join(n.Ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: n.Config.ID, Addr: n.Address},
	})
	require.NoError(t, err, "can't join existing Raft")

	n.RegisterNodes(resp.Members)

	Register(s, n)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, s.Serve(wrappedListener))
	}()
	return &testNode{Node: n, listener: wrappedListener}
}

func newRaftCluster(t *testing.T, opts ...NewNodeOptions) map[uint64]*testNode {
	nodes := make(map[uint64]*testNode)
	nodes[1] = newInitNode(t, opts...)
	addRaftNode(t, nodes, opts...)
	addRaftNode(t, nodes, opts...)
	return nodes
}

func addRaftNode(t *testing.T, nodes map[uint64]*testNode, opts ...NewNodeOptions) {
	n := uint64(len(nodes) + 1)
	nodes[n] = newJoinNode(t, nodes[1].Address, opts...)
	waitForCluster(t, nodes)
}

func teardownCluster(t *testing.T, nodes map[uint64]*testNode) {
	for _, node := range nodes {
		shutdownNode(node)
		node.listener.close()
	}
}

func removeNode(nodes map[string]*testNode, node string) {
	shutdownNode(nodes[node])
	delete(nodes, node)
}

func shutdownNode(node *testNode) {
	node.Server.Stop()
	node.Shutdown()
	os.RemoveAll(node.stateDir)
}

func TestRaftBootstrap(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.Equal(t, len(nodes[1].cluster.Members()), 3)
	assert.Equal(t, len(nodes[2].cluster.Members()), 3)
	assert.Equal(t, len(nodes[3].cluster.Members()), 3)
}

func TestRaftLeader(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].Config.ID)
}

func proposeValue(t *testing.T, raftNode *testNode, nodeID ...string) (*api.Node, error) {
	nodeIDStr := "id1"
	if len(nodeID) != 0 {
		nodeIDStr = nodeID[0]
	}
	node := &api.Node{
		ID: nodeIDStr,
		Spec: &api.NodeSpec{
			Meta: api.Meta{
				Name: nodeIDStr,
			},
		},
	}

	storeActions := []*api.StoreAction{
		{
			Action: &api.StoreAction_CreateNode{
				CreateNode: node,
			},
		},
	}

	err := raftNode.ProposeValue(context.Background(), storeActions, func() {
		err := raftNode.memoryStore.applyStoreActions(storeActions)
		assert.NoError(t, err, "error applying actions")
	})
	if err != nil {
		return nil, err
	}

	return node, nil
}

func checkValue(t *testing.T, raftNode *testNode, createdNode *api.Node) {
	err := raftNode.memoryStore.View(func(tx ReadTx) error {
		allNodes, err := tx.Nodes().Find(All)
		if err != nil {
			return err
		}
		assert.Len(t, allNodes, 1)
		assert.Equal(t, allNodes[0], createdNode)
		return nil
	})
	assert.NoError(t, err)
}

func checkNoValue(t *testing.T, raftNode *testNode) {
	err := raftNode.memoryStore.View(func(tx ReadTx) error {
		allNodes, err := tx.Nodes().Find(All)
		if err != nil {
			return err
		}
		assert.Len(t, allNodes, 0)
		return nil
	})
	assert.NoError(t, err)
}

func TestRaftLeaderDown(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop node 1
	nodes[1].Stop()

	newCluster := map[uint64]*testNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for the re-election to occur
	waitForCluster(t, newCluster)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].Config.ID)

	// Ensure that node 2 and node 3 have the same leader
	assert.Equal(t, nodes[3].Leader(), nodes[2].Leader())

	// Find the leader node and a follower node
	var (
		leaderNode   *testNode
		followerNode *testNode
	)
	for i, n := range newCluster {
		if n.Config.ID == n.Leader() {
			leaderNode = n
			if i == 2 {
				followerNode = newCluster[3]
			} else {
				followerNode = newCluster[2]
			}
		}
	}

	require.NotNil(t, leaderNode)
	require.NotNil(t, followerNode)

	// Propose a value
	value, err := proposeValue(t, leaderNode)
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, leaderNode, value)
	assert.Equal(t, len(leaderNode.cluster.Members()), 3)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, followerNode, value)
	assert.Equal(t, len(followerNode.cluster.Members()), 3)
}

func TestRaftFollowerDown(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop node 3
	nodes[3].Stop()

	// Leader should still be 1
	assert.True(t, nodes[1].IsLeader(), "node 1 is not a leader anymore")
	assert.Equal(t, nodes[2].Leader(), nodes[1].Config.ID)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, nodes[1], value)
	assert.Equal(t, len(nodes[1].cluster.Members()), 3)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].cluster.Members()), 3)
}

func TestRaftLogReplication(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	checkValue(t, nodes[1], value)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)
	checkValue(t, nodes[3], value)
}

func TestRaftLogReplicationWithoutLeader(t *testing.T) {
	t.Parallel()
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop the leader
	nodes[1].Stop()

	// Propose a value
	_, err := proposeValue(t, nodes[2])
	assert.Error(t, err)

	// No value should be replicated in the store in the absence of the leader
	checkNoValue(t, nodes[2])

	time.Sleep(500 * time.Millisecond)

	checkNoValue(t, nodes[3])
}

func TestRaftQuorumFailure(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes := newRaftCluster(t)
	addRaftNode(t, nodes)
	addRaftNode(t, nodes)
	defer teardownCluster(t, nodes)

	// Lose a majority
	nodes[3].Stop()
	nodes[4].Stop()
	nodes[5].Stop()

	// Propose a value
	_, err := proposeValue(t, nodes[2])
	assert.Error(t, err)

	// The value should not be replicated, we have no majority
	checkNoValue(t, nodes[2])

	time.Sleep(500 * time.Millisecond)

	checkNoValue(t, nodes[1])
}

func TestRaftFollowerLeave(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes := newRaftCluster(t)
	addRaftNode(t, nodes)
	addRaftNode(t, nodes)
	defer teardownCluster(t, nodes)

	// Node 5 leave the cluster
	resp, err := nodes[5].Leave(nodes[5].Ctx, &api.LeaveRequest{Node: &api.RaftNode{ID: nodes[5].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	delete(nodes, 5)

	waitForPeerNumber(t, nodes, 4)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// Value should be replicated on every node
	checkValue(t, nodes[1], value)
	assert.Equal(t, len(nodes[1].cluster.Members()), 4)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].cluster.Members()), 4)

	checkValue(t, nodes[3], value)
	assert.Equal(t, len(nodes[3].cluster.Members()), 4)

	checkValue(t, nodes[4], value)
	assert.Equal(t, len(nodes[4].cluster.Members()), 4)
}

func TestRaftLeaderLeave(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// node 1 is the leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)

	// Try to leave the raft
	resp, err := nodes[1].Leave(nodes[1].Ctx, &api.LeaveRequest{Node: &api.RaftNode{ID: nodes[1].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	newCluster := map[uint64]*testNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for election tick
	waitForCluster(t, newCluster)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[2].Leader(), nodes[3].Leader())

	leader := nodes[2].Leader()

	// Find the leader node and a follower node
	var (
		leaderNode   *testNode
		followerNode *testNode
	)
	for i, n := range nodes {
		if n.Config.ID == leader {
			leaderNode = n
			if i == 2 {
				followerNode = nodes[3]
			} else {
				followerNode = nodes[2]
			}
		}
	}

	require.NotNil(t, leaderNode)
	require.NotNil(t, followerNode)

	// Propose a value
	value, err := proposeValue(t, leaderNode)
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, leaderNode, value)
	assert.Equal(t, len(leaderNode.cluster.Members()), 2)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, followerNode, value)
	assert.Equal(t, len(followerNode.cluster.Members()), 2)
}

func TestRaftNewNodeGetsData(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// Add a new node
	addRaftNode(t, nodes)

	time.Sleep(500 * time.Millisecond)

	// Value should be replicated on every node
	for _, node := range nodes {
		checkValue(t, node, value)
		assert.Equal(t, len(node.cluster.Members()), 4)
	}
}

func TestRaftSnapshot(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	var zero uint64
	nodes := newRaftCluster(t, NewNodeOptions{SnapshotInterval: 9, LogEntriesForSlowFollowers: &zero})
	defer teardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5"}
	values := make([]*api.Node, len(nodeIDs))

	// Propose 4 values
	for i, nodeID := range nodeIDs[:4] {
		var err error
		values[i], err = proposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// None of the nodes should have snapshot files yet
	for _, node := range nodes {
		dirents, err := ioutil.ReadDir(filepath.Join(node.stateDir, "snap"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Propose a 5th value
	var err error
	values[4], err = proposeValue(t, nodes[1], nodeIDs[4])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should now have a snapshot file
	time.Sleep(500 * time.Millisecond)
	for _, node := range nodes {
		dirents, err := ioutil.ReadDir(filepath.Join(node.stateDir, "snap"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 1)
	}

	// Add a node to the cluster
	addRaftNode(t, nodes)

	// It should get a copy of the snapshot
	time.Sleep(500 * time.Millisecond)
	dirents, err := ioutil.ReadDir(filepath.Join(nodes[4].stateDir, "snap"))
	assert.NoError(t, err)
	assert.Len(t, dirents, 1)

	// All nodes should have all the data
	for _, node := range nodes {
		err = node.memoryStore.View(func(tx ReadTx) error {
			allNodes, err := tx.Nodes().Find(All)
			if err != nil {
				return err
			}
			assert.Len(t, allNodes, 5)

			for i, nodeID := range nodeIDs {
				n := tx.Nodes().Get(nodeID)
				assert.Equal(t, n, values[i])
			}
			return nil
		})
		assert.NoError(t, err)
	}
}

func TestRaftRejoin(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	values := make([]*api.Node, 2)
	var err error
	values[0], err = proposeValue(t, nodes[1], "id1")
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on node 3
	time.Sleep(500 * time.Millisecond)
	checkValue(t, nodes[3], values[0])
	assert.Equal(t, len(nodes[3].cluster.Members()), 3)

	// Stop node 3
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Propose another value
	values[1], err = proposeValue(t, nodes[1], "id2")
	assert.NoError(t, err, "failed to propose value")

	time.Sleep(2 * time.Second)

	// Nodes 1 and 2 should have the new value
	checkValues := func(checkNodes ...*testNode) {
		for _, node := range checkNodes {
			err := node.memoryStore.View(func(tx ReadTx) error {
				allNodes, err := tx.Nodes().Find(All)
				if err != nil {
					return err
				}
				assert.Len(t, allNodes, 2)

				for i, nodeID := range []string{"id1", "id2"} {
					n := tx.Nodes().Get(nodeID)
					assert.Equal(t, n, values[i])
				}
				return nil
			})
			assert.NoError(t, err)
		}
	}
	checkValues(nodes[1], nodes[2])

	nodes[3] = restartNode(t, nodes[3], nodes[1].Address)
	waitForCluster(t, nodes)

	// Node 3 should have all values, including the one proposed while
	// it was unavailable.
	time.Sleep(500 * time.Millisecond)
	checkValues(nodes[1], nodes[2], nodes[3])
}

func TestRaftUnreachableNode(t *testing.T) {
	t.Parallel()

	nodes := make(map[uint64]*testNode)
	nodes[1] = newInitNode(t)

	// Add a new node, but don't start its server yet
	n := newNode(t)

	c, err := GetRaftClient(nodes[1].Address, 500*time.Millisecond)
	assert.NoError(t, err, "can't initiate connection with existing raft")

	resp, err := c.Join(n.Ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: n.Config.ID, Addr: n.Address},
	})
	require.NoError(t, err, "can't join existing Raft")

	time.Sleep(5 * time.Second)

	n.Start()

	n.RegisterNodes(resp.Members)

	Register(n.Server, n.Node)

	// Now start the new node's server
	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()

	nodes[2] = n
	waitForCluster(t, nodes)

	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	checkValue(t, nodes[1], value)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)
}
