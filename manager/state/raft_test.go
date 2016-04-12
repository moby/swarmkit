package state

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/coreos/etcd/raft"
	managerpb "github.com/docker/swarm-v2/pb/docker/cluster/api/manager"
	raftpb "github.com/docker/swarm-v2/pb/docker/cluster/api/raft"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	specspb "github.com/docker/swarm-v2/pb/docker/cluster/specs"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

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

func advanceTicks(clockSource *fakeclock.FakeClock, ticks int) {
	// A FakeClock timer won't fire multiple times if time is advanced
	// more than its interval.
	for i := 0; i != ticks; i++ {
		clockSource.Increment(time.Second)
	}
}

func pollFunc(f func() error) error {
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

// waitForCluster waits until leader will be one of specified nodes
func waitForCluster(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*testNode) {
	err := pollFunc(func() error {
		clockSource.Increment(time.Second)
		var prev *raft.Status
	nodeLoop:
		for _, n := range nodes {
			if prev == nil {
				prev = new(raft.Status)
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

// waitForPeerNumber waits until peers in cluster converge to specified number
func waitForPeerNumber(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*testNode, count int) {
	assert.NoError(t, pollFunc(func() error {
		clockSource.Increment(time.Second)
		for _, n := range nodes {
			if len(n.cluster.listMembers()) != count {
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

func newNode(t *testing.T, clockSource *fakeclock.FakeClock, opts ...NewNodeOptions) *testNode {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "can't bind to raft service port")
	wrappedListener := newWrappedListener(l)
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		Addr:        l.Addr().String(),
		Config:      cfg,
		StateDir:    stateDir,
		ClockSource: clockSource,
		SendTimeout: 100 * time.Millisecond,
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

	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	return &testNode{Node: n, listener: wrappedListener}
}

func newInitNode(t *testing.T, opts ...NewNodeOptions) (*testNode, *fakeclock.FakeClock) {
	clockSource := fakeclock.NewFakeClock(time.Now())
	n := newNode(t, clockSource, opts...)

	go n.Run()

	Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()

	return n, clockSource
}

func newJoinNode(t *testing.T, clockSource *fakeclock.FakeClock, join string, opts ...NewNodeOptions) *testNode {
	var derivedOpts NewNodeOptions
	if len(opts) == 1 {
		derivedOpts = opts[0]
	}
	derivedOpts.JoinAddr = join
	n := newNode(t, clockSource, derivedOpts)

	go n.Run()
	Register(n.Server, n.Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()
	return n
}

func restartNode(t *testing.T, clockSource *fakeclock.FakeClock, oldNode *testNode) *testNode {
	wrappedListener := recycleWrappedListener(oldNode.listener)
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	newNodeOpts := NewNodeOptions{
		Addr:        oldNode.Address,
		Config:      cfg,
		StateDir:    oldNode.stateDir,
		ClockSource: clockSource,
		SendTimeout: 100 * time.Millisecond,
	}

	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Server = s

	go n.Run()

	Register(s, n)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, s.Serve(wrappedListener))
	}()
	return &testNode{Node: n, listener: wrappedListener}
}

func newRaftCluster(t *testing.T, opts ...NewNodeOptions) (map[uint64]*testNode, *fakeclock.FakeClock) {
	nodes := make(map[uint64]*testNode)
	var clockSource *fakeclock.FakeClock
	nodes[1], clockSource = newInitNode(t, opts...)
	addRaftNode(t, clockSource, nodes, opts...)
	addRaftNode(t, clockSource, nodes, opts...)
	return nodes, clockSource
}

func addRaftNode(t *testing.T, clockSource *fakeclock.FakeClock, nodes map[uint64]*testNode, opts ...NewNodeOptions) {
	n := uint64(len(nodes) + 1)
	nodes[n] = newJoinNode(t, clockSource, nodes[1].Address, opts...)
	waitForCluster(t, clockSource, nodes)
}

func teardownCluster(t *testing.T, nodes map[uint64]*testNode) {
	for _, node := range nodes {
		shutdownNode(node)
		node.listener.close()
	}
}

func shutdownNode(node *testNode) {
	node.Server.Stop()
	node.Shutdown()
	os.RemoveAll(node.stateDir)
}

func leader(nodes map[uint64]*testNode) *testNode {
	for _, n := range nodes {
		if n.Config.ID == n.Leader() {
			return n
		}
	}
	panic("could not find a leader")
}

func TestRaftBootstrap(t *testing.T) {
	t.Parallel()

	nodes, _ := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.Equal(t, 3, len(nodes[1].cluster.listMembers()))
	assert.Equal(t, 3, len(nodes[2].cluster.listMembers()))
	assert.Equal(t, 3, len(nodes[3].cluster.listMembers()))
}

func TestRaftLeader(t *testing.T) {
	t.Parallel()

	nodes, _ := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].Config.ID)
}

func proposeValue(t *testing.T, raftNode *testNode, nodeID ...string) (*objectspb.Node, error) {
	nodeIDStr := "id1"
	if len(nodeID) != 0 {
		nodeIDStr = nodeID[0]
	}
	node := &objectspb.Node{
		ID: nodeIDStr,
		Spec: &specspb.NodeSpec{
			Meta: specspb.Meta{
				Name: nodeIDStr,
			},
		},
	}

	storeActions := []*raftpb.StoreAction{
		{
			Action: raftpb.StoreActionKindCreate,
			Target: &raftpb.StoreAction_Node{
				Node: node,
			},
		},
	}

	ctx, _ := context.WithTimeout(context.Background(), time.Second)

	err := raftNode.ProposeValue(ctx, storeActions, func() {
		err := raftNode.memoryStore.applyStoreActions(storeActions)
		assert.NoError(t, err, "error applying actions")
	})
	if err != nil {
		return nil, err
	}

	return node, nil
}

func checkValue(t *testing.T, raftNode *testNode, createdNode *objectspb.Node) {
	assert.NoError(t, pollFunc(func() error {
		return raftNode.memoryStore.View(func(tx ReadTx) error {
			allNodes, err := tx.Nodes().Find(All)
			if err != nil {
				return err
			}
			if len(allNodes) != 1 {
				return fmt.Errorf("expected 1 node, got %d nodes", len(allNodes))
			}
			if !reflect.DeepEqual(allNodes[0], createdNode) {
				return errors.New("node did not match expected value")
			}
			return nil
		})
	}))
}

func checkNoValue(t *testing.T, raftNode *testNode) {
	assert.NoError(t, pollFunc(func() error {
		return raftNode.memoryStore.View(func(tx ReadTx) error {
			allNodes, err := tx.Nodes().Find(All)
			if err != nil {
				return err
			}
			if len(allNodes) != 0 {
				return fmt.Errorf("expected no nodes, got %d", len(allNodes))
			}
			return nil
		})
	}))
}

func checkValuesOnNodes(t *testing.T, checkNodes map[uint64]*testNode, ids []string, values []*objectspb.Node) {
	for _, node := range checkNodes {
		assert.NoError(t, pollFunc(func() error {
			return node.memoryStore.View(func(tx ReadTx) error {
				allNodes, err := tx.Nodes().Find(All)
				if err != nil {
					return err
				}
				if len(allNodes) != len(ids) {
					return fmt.Errorf("expected %d nodes, got %d", len(ids), len(allNodes))
				}

				for i, id := range ids {
					n := tx.Nodes().Get(id)
					if !reflect.DeepEqual(values[i], n) {
						return fmt.Errorf("node %s did not match expected value", id)
					}
				}
				return nil
			})
		}))
	}
}

func TestRaftLeaderDown(t *testing.T) {
	t.Parallel()

	nodes, clockSource := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop node 1
	nodes[1].Stop()

	newCluster := map[uint64]*testNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for the re-election to occur
	waitForCluster(t, clockSource, newCluster)

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
	assert.Equal(t, len(leaderNode.cluster.listMembers()), 3)

	checkValue(t, followerNode, value)
	assert.Equal(t, len(followerNode.cluster.listMembers()), 3)
}

func TestRaftFollowerDown(t *testing.T) {
	t.Parallel()

	nodes, _ := newRaftCluster(t)
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
	assert.Equal(t, len(nodes[1].cluster.listMembers()), 3)

	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].cluster.listMembers()), 3)
}

func TestRaftLogReplication(t *testing.T) {
	t.Parallel()

	nodes, _ := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	checkValue(t, nodes[1], value)
	checkValue(t, nodes[2], value)
	checkValue(t, nodes[3], value)
}

func TestRaftLogReplicationWithoutLeader(t *testing.T) {
	t.Parallel()
	nodes, _ := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop the leader
	nodes[1].Stop()

	// Propose a value
	_, err := proposeValue(t, nodes[2])
	assert.Error(t, err)

	// No value should be replicated in the store in the absence of the leader
	checkNoValue(t, nodes[2])
	checkNoValue(t, nodes[3])
}

func TestRaftQuorumFailure(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := newRaftCluster(t)
	addRaftNode(t, clockSource, nodes)
	addRaftNode(t, clockSource, nodes)
	defer teardownCluster(t, nodes)

	// Lose a majority
	for i := uint64(3); i <= 5; i++ {
		nodes[i].Server.Stop()
		nodes[i].Stop()
	}

	// Propose a value
	_, err := proposeValue(t, nodes[1])
	assert.Error(t, err)

	// The value should not be replicated, we have no majority
	checkNoValue(t, nodes[2])
	checkNoValue(t, nodes[1])
}

func TestRaftQuorumRecovery(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := newRaftCluster(t)
	addRaftNode(t, clockSource, nodes)
	addRaftNode(t, clockSource, nodes)
	defer teardownCluster(t, nodes)

	// Lose a majority
	for i := uint64(1); i <= 3; i++ {
		nodes[i].Server.Stop()
		nodes[i].Shutdown()
	}

	advanceTicks(clockSource, 5)

	// Restore the majority by restarting node 3
	nodes[3] = restartNode(t, clockSource, nodes[3])

	delete(nodes, 1)
	delete(nodes, 2)
	waitForCluster(t, clockSource, nodes)

	// Propose a value
	value, err := proposeValue(t, leader(nodes))
	assert.NoError(t, err)

	for _, node := range nodes {
		checkValue(t, node, value)
	}
}

func TestRaftFollowerLeave(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := newRaftCluster(t)
	addRaftNode(t, clockSource, nodes)
	addRaftNode(t, clockSource, nodes)
	defer teardownCluster(t, nodes)

	// Node 5 leave the cluster
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := nodes[1].Leave(ctx, &managerpb.LeaveRequest{Node: &managerpb.RaftNode{ID: nodes[5].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	delete(nodes, 5)

	waitForPeerNumber(t, clockSource, nodes, 4)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// Value should be replicated on every node
	checkValue(t, nodes[1], value)
	assert.Equal(t, len(nodes[1].cluster.listMembers()), 4)

	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].cluster.listMembers()), 4)

	checkValue(t, nodes[3], value)
	assert.Equal(t, len(nodes[3].cluster.listMembers()), 4)

	checkValue(t, nodes[4], value)
	assert.Equal(t, len(nodes[4].cluster.listMembers()), 4)
}

func TestRaftLeaderLeave(t *testing.T) {
	t.Parallel()

	nodes, clockSource := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// node 1 is the leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)

	// Try to leave the raft
	resp, err := nodes[1].Leave(nodes[1].Ctx, &managerpb.LeaveRequest{Node: &managerpb.RaftNode{ID: nodes[1].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	newCluster := map[uint64]*testNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for election tick
	waitForCluster(t, clockSource, newCluster)

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
	assert.Equal(t, len(leaderNode.cluster.listMembers()), 2)

	checkValue(t, followerNode, value)
	assert.Equal(t, len(followerNode.cluster.listMembers()), 2)
}

func TestRaftNewNodeGetsData(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes, clockSource := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// Add a new node
	addRaftNode(t, clockSource, nodes)

	// Value should be replicated on every node
	for _, node := range nodes {
		checkValue(t, node, value)
		assert.Equal(t, len(node.cluster.listMembers()), 4)
	}
}

func TestRaftSnapshot(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	var zero uint64
	nodes, clockSource := newRaftCluster(t, NewNodeOptions{SnapshotInterval: 9, LogEntriesForSlowFollowers: &zero})
	defer teardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5"}
	values := make([]*objectspb.Node, len(nodeIDs))

	// Propose 4 values
	var err error
	for i, nodeID := range nodeIDs[:4] {
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
	values[4], err = proposeValue(t, nodes[1], nodeIDs[4])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should now have a snapshot file
	for _, node := range nodes {
		assert.NoError(t, pollFunc(func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.stateDir, "snap"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
			}
			return nil
		}))
	}

	// Add a node to the cluster
	addRaftNode(t, clockSource, nodes)

	// It should get a copy of the snapshot
	assert.NoError(t, pollFunc(func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[4].stateDir, "snap"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}
		return nil
	}))

	// It should know about the other nodes
	nodesFromMembers := func(memberList map[uint64]*member) map[uint64]*managerpb.RaftNode {
		raftNodes := make(map[uint64]*managerpb.RaftNode)
		for k, v := range memberList {
			raftNodes[k] = v.RaftNode
		}
		return raftNodes
	}
	assert.Equal(t, nodesFromMembers(nodes[1].cluster.listMembers()), nodesFromMembers(nodes[4].cluster.listMembers()))

	// All nodes should have all the data
	checkValuesOnNodes(t, nodes, nodeIDs, values)
}

func TestRaftSnapshotRestart(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	var zero uint64
	nodes, clockSource := newRaftCluster(t, NewNodeOptions{SnapshotInterval: 10, LogEntriesForSlowFollowers: &zero})
	defer teardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5", "id6", "id7", "id8"}
	values := make([]*objectspb.Node, len(nodeIDs))

	// Propose 4 values
	var err error
	for i, nodeID := range nodeIDs[:4] {
		values[i], err = proposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// Take down node 3
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Propose a 5th value before the snapshot
	values[4], err = proposeValue(t, nodes[1], nodeIDs[4])
	assert.NoError(t, err, "failed to propose value")

	// Remaining nodes shouldn't have snapshot files yet
	for _, node := range []*testNode{nodes[1], nodes[2]} {
		dirents, err := ioutil.ReadDir(filepath.Join(node.stateDir, "snap"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Add a node to the cluster before the snapshot. This is the event
	// that triggers the snapshot.
	nodes[4] = newJoinNode(t, clockSource, nodes[1].Address)
	waitForCluster(t, clockSource, map[uint64]*testNode{1: nodes[1], 2: nodes[2], 4: nodes[4]})

	// Remaining nodes should now have a snapshot file
	for _, node := range []*testNode{nodes[1], nodes[2]} {
		assert.NoError(t, pollFunc(func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.stateDir, "snap"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
			}
			return nil
		}))
	}
	checkValuesOnNodes(t, map[uint64]*testNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:5], values[:5])

	// Propose a 6th value
	values[5], err = proposeValue(t, nodes[1], nodeIDs[5])

	// Add another node to the cluster
	nodes[5] = newJoinNode(t, clockSource, nodes[1].Address)
	waitForCluster(t, clockSource, map[uint64]*testNode{1: nodes[1], 2: nodes[2], 4: nodes[4], 5: nodes[5]})

	// New node should get a copy of the snapshot
	assert.NoError(t, pollFunc(func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].stateDir, "snap"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}
		return nil
	}))

	dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].stateDir, "snap"))
	assert.NoError(t, err)
	assert.Len(t, dirents, 1)
	checkValuesOnNodes(t, map[uint64]*testNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:6], values[:6])

	// It should know about the other nodes, including the one that was just added
	nodesFromMembers := func(memberList map[uint64]*member) map[uint64]*managerpb.RaftNode {
		raftNodes := make(map[uint64]*managerpb.RaftNode)
		for k, v := range memberList {
			raftNodes[k] = v.RaftNode
		}
		return raftNodes
	}
	assert.Equal(t, nodesFromMembers(nodes[1].cluster.listMembers()), nodesFromMembers(nodes[4].cluster.listMembers()))

	// Restart node 3
	nodes[3] = restartNode(t, clockSource, nodes[3])
	waitForCluster(t, clockSource, nodes)

	// Node 3 should know about other nodes, including the new one
	assert.Len(t, nodes[3].cluster.listMembers(), 5)
	assert.Equal(t, nodesFromMembers(nodes[1].cluster.listMembers()), nodesFromMembers(nodes[3].cluster.listMembers()))

	// Propose yet another value, to make sure the rejoined node is still
	// receiving new logs
	values[6], err = proposeValue(t, nodes[1], nodeIDs[6])

	// All nodes should have all the data
	checkValuesOnNodes(t, nodes, nodeIDs[:7], values[:7])

	// Restart node 3 again. It should load the snapshot.
	nodes[3].Server.Stop()
	nodes[3].Shutdown()
	nodes[3] = restartNode(t, clockSource, nodes[3])
	waitForCluster(t, clockSource, nodes)

	assert.Len(t, nodes[3].cluster.listMembers(), 5)
	assert.Equal(t, nodesFromMembers(nodes[1].cluster.listMembers()), nodesFromMembers(nodes[3].cluster.listMembers()))
	checkValuesOnNodes(t, nodes, nodeIDs[:7], values[:7])

	// Propose again. Just to check consensus after this latest restart.
	values[7], err = proposeValue(t, nodes[1], nodeIDs[7])
	checkValuesOnNodes(t, nodes, nodeIDs, values)
}

func TestRaftRejoin(t *testing.T) {
	t.Parallel()

	nodes, clockSource := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	ids := []string{"id1", "id2"}

	// Propose a value
	values := make([]*objectspb.Node, 2)
	var err error
	values[0], err = proposeValue(t, nodes[1], ids[0])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on node 3
	checkValue(t, nodes[3], values[0])
	assert.Equal(t, len(nodes[3].cluster.listMembers()), 3)

	// Stop node 3
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Propose another value
	values[1], err = proposeValue(t, nodes[1], ids[1])
	assert.NoError(t, err, "failed to propose value")

	// Nodes 1 and 2 should have the new value
	checkValuesOnNodes(t, map[uint64]*testNode{1: nodes[1], 2: nodes[2]}, ids, values)

	nodes[3] = restartNode(t, clockSource, nodes[3])
	waitForCluster(t, clockSource, nodes)

	// Node 3 should have all values, including the one proposed while
	// it was unavailable.
	checkValuesOnNodes(t, nodes, ids, values)
}

func testRaftRestartCluster(t *testing.T, stagger bool) {
	nodes, clockSource := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Propose a value
	values := make([]*objectspb.Node, 2)
	var err error
	values[0], err = proposeValue(t, nodes[1], "id1")
	assert.NoError(t, err, "failed to propose value")

	// Stop all nodes
	for _, node := range nodes {
		node.Server.Stop()
		node.Shutdown()
	}

	advanceTicks(clockSource, 5)

	// Restart all nodes
	i := 0
	for k, node := range nodes {
		if stagger && i != 0 {
			advanceTicks(clockSource, 1)
		}
		nodes[k] = restartNode(t, clockSource, node)
		i++
	}
	waitForCluster(t, clockSource, nodes)

	// Propose another value
	values[1], err = proposeValue(t, leader(nodes), "id2")
	assert.NoError(t, err, "failed to propose value")

	for _, node := range nodes {
		assert.NoError(t, pollFunc(func() error {
			return node.memoryStore.View(func(tx ReadTx) error {
				allNodes, err := tx.Nodes().Find(All)
				if err != nil {
					return err
				}
				if len(allNodes) != 2 {
					return fmt.Errorf("expected 2 nodes, got %d", len(allNodes))
				}

				for i, nodeID := range []string{"id1", "id2"} {
					n := tx.Nodes().Get(nodeID)
					if !reflect.DeepEqual(n, values[i]) {
						return fmt.Errorf("node %s did not match expected value", nodeID)
					}
				}
				return nil
			})
		}))
	}
}

func TestRaftRestartClusterSimultaneously(t *testing.T) {
	t.Parallel()

	// Establish a cluster, stop all nodes (simulating a total outage), and
	// restart them simultaneously.
	testRaftRestartCluster(t, false)
}

func TestRaftRestartClusterStaggered(t *testing.T) {
	t.Parallel()

	// Establish a cluster, stop all nodes (simulating a total outage), and
	// restart them one at a time.
	testRaftRestartCluster(t, true)
}

func TestRaftUnreachableNode(t *testing.T) {
	t.Parallel()

	nodes := make(map[uint64]*testNode)
	var clockSource *fakeclock.FakeClock
	nodes[1], clockSource = newInitNode(t)

	// Add a new node, but don't start its server yet
	n := newNode(t, clockSource, NewNodeOptions{JoinAddr: nodes[1].Address})
	go n.Run()

	advanceTicks(clockSource, 5)
	time.Sleep(100 * time.Millisecond)

	Register(n.Server, n.Node)

	// Now start the new node's server
	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, n.Server.Serve(n.listener))
	}()

	nodes[2] = n
	waitForCluster(t, clockSource, nodes)

	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	checkValue(t, nodes[1], value)
	checkValue(t, nodes[2], value)
}

func TestRaftJoinFollower(t *testing.T) {
	t.Parallel()

	nodes := make(map[uint64]*testNode)
	var clockSource *fakeclock.FakeClock
	nodes[1], clockSource = newInitNode(t)
	nodes[2] = newJoinNode(t, clockSource, nodes[1].Address)
	waitForCluster(t, clockSource, nodes)

	// Point new node at a follower's address, rather than the leader
	nodes[3] = newJoinNode(t, clockSource, nodes[2].Address)
	waitForCluster(t, clockSource, nodes)
	defer teardownCluster(t, nodes)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	checkValue(t, nodes[1], value)
	checkValue(t, nodes[2], value)
	checkValue(t, nodes[3], value)
}
