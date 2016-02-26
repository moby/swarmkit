package raft

import (
	"io/ioutil"
	"log"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/coreos/etcd/raft"
	"github.com/docker/swarm-v2/state/raft/pb"
	"github.com/stretchr/testify/assert"
)

var (
	raftLogger = &raft.DefaultLogger{Logger: log.New(ioutil.Discard, "", 0)}
)

// Main Raft test method, we execute the
// tests sequentially to avoid any issue
// on bootstrap and raft cluster lifecycle
func TestRaft(t *testing.T) {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))

	testBootstrap(t)
	testLeader(t)
	testLeaderDown(t)
	testFollowerDown(t)
	testLogReplication(t)
	testLogReplicationWithoutLeader(t)
	testQuorumFailure(t)
	testLeaderLeave(t)
	testFollowerLeave(t)

	// TODO
	testSnapshot(t)
	testRecoverSnapshot(t)
}

func newInitNode(t *testing.T, id uint64) *Node {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err, "Can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	n, err := NewNode(id, l.Addr().String(), cfg, nil)
	assert.NoError(t, err, "Can't create raft node")
	n.Listener = l
	n.Server = s

	n.Campaign(n.Ctx)
	go n.Start()

	Register(s, n)
	go s.Serve(l)

	time.Sleep(1 * time.Second)
	return n
}

func newJoinNode(t *testing.T, id uint64, join string) *Node {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err, "Can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	n, err := NewNode(id, l.Addr().String(), cfg, nil)
	assert.NoError(t, err, "Can't create raft node")
	n.Listener = l
	n.Server = s

	go n.Start()

	c, err := GetRaftClient(join, 100*time.Millisecond)
	assert.NoError(t, err, "Can't initiate connection with existing raft")

	resp, err := c.JoinRaft(n.Ctx, &pb.NodeInfo{ID: id, Addr: l.Addr().String()})
	assert.NoError(t, err, "Can't join existing Raft")

	err = n.RegisterNodes(resp.Nodes)
	assert.NoError(t, err, "Can't add nodes to the local cluster list")

	Register(s, n)
	go s.Serve(l)

	time.Sleep(1 * time.Second)
	return n
}

func newRaftCluster(t *testing.T) map[int]*Node {
	nodes := make(map[int]*Node, 0)

	nodes[1] = newInitNode(t, 1)
	nodes[2] = newJoinNode(t, 2, nodes[1].Listener.Addr().String())
	nodes[3] = newJoinNode(t, 3, nodes[1].Listener.Addr().String())

	return nodes
}

func addRaftNode(t *testing.T, nodes map[int]*Node) {
	n := len(nodes) + 1
	nodes[n] = newJoinNode(t, uint64(n), nodes[1].Listener.Addr().String())
}

func teardownCluster(t *testing.T, nodes map[int]*Node) {
	for _, node := range nodes {
		shutdownNode(node)
	}
	nodes = nil

	// FIXME We have to wait a little bit for the
	// connections to be cleaned up properly
	time.Sleep(2 * time.Second)
}

func removeNode(nodes map[string]*Node, node string) {
	shutdownNode(nodes[node])
	delete(nodes, node)
}

func shutdownNode(node *Node) {
	node.Server.Stop()
	node.Server.TestingCloseConns()
	node.Listener.Close()
	node.Listener = nil
	node.Shutdown()
}

func testBootstrap(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.Equal(t, len(nodes[1].Cluster.Peers()), 3)
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 3)
}

func testLeader(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].ID)
}

func testLeaderDown(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for failure test")

	// Stop node 1
	nodes[1].Stop()

	// Wait for the re-election to occur
	time.Sleep(4 * time.Second)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].ID)

	// Ensure that node 2 and node 3 have the same leader
	assert.Equal(t, nodes[3].Leader(), nodes[2].Leader())

	// Propose a value
	nodes[2].Propose(nodes[2].Ctx, pair)

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// The value should be replicated on all remaining nodes
	assert.Equal(t, nodes[2].StoreLength(), 1)
	assert.Equal(t, nodes[2].Get(key), string(value))
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)

	assert.Equal(t, nodes[3].StoreLength(), 1)
	assert.Equal(t, nodes[3].Get(key), string(value))
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 3)
}

func testFollowerDown(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for failure test")

	// Stop node 3
	nodes[3].Stop()

	// Wait election tick
	time.Sleep(4 * time.Second)

	// Leader should still be 1
	assert.True(t, nodes[1].IsLeader(), "node 1 is not a leader anymore")
	assert.Equal(t, nodes[2].Leader(), nodes[1].ID)

	// Propose a value
	nodes[2].Propose(nodes[2].Ctx, pair)

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// The value should be replicated on all remaining nodes
	assert.Equal(t, nodes[1].StoreLength(), 1)
	assert.Equal(t, nodes[1].Get(key), string(value))
	assert.Equal(t, len(nodes[1].Cluster.Peers()), 3)

	assert.Equal(t, nodes[2].StoreLength(), 1)
	assert.Equal(t, nodes[2].Get(key), string(value))
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)
}

func testLogReplication(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for replication test")

	// Propose a value
	err = nodes[1].Propose(nodes[1].Ctx, pair)
	assert.NoError(t, err, "Can't propose new value")

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// All nodes should have the value in the physical store
	assert.Equal(t, nodes[1].StoreLength(), 1)
	assert.Equal(t, nodes[1].Get(key), string(value))

	assert.Equal(t, nodes[2].StoreLength(), 1)
	assert.Equal(t, nodes[2].Get(key), string(value))

	assert.Equal(t, nodes[3].StoreLength(), 1)
	assert.Equal(t, nodes[3].Get(key), string(value))
}

func testLogReplicationWithoutLeader(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for replication test")

	// Stop the leader
	nodes[1].Stop()

	// Propose a value
	err = nodes[2].Propose(nodes[2].Ctx, pair)
	assert.NoError(t, err, "Can't propose new value")

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// No value should be replicated in the store in the absence of the leader
	assert.Equal(t, nodes[2].StoreLength(), 0)
	assert.Equal(t, nodes[2].Get(key), "")

	assert.Equal(t, nodes[3].StoreLength(), 0)
	assert.Equal(t, nodes[3].Get(key), "")
}

func testQuorumFailure(t *testing.T) {
	// Bring up a 5 nodes cluster
	nodes := newRaftCluster(t)
	addRaftNode(t, nodes)
	addRaftNode(t, nodes)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for failure test")

	// Lose a majority
	nodes[3].Stop()
	nodes[4].Stop()
	nodes[5].Stop()

	// Propose a value
	nodes[1].Propose(nodes[1].Ctx, pair)

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// The value should not be replicated, we have no majority
	assert.Equal(t, nodes[1].StoreLength(), 0)
	assert.Equal(t, nodes[1].Get(key), "")

	assert.Equal(t, nodes[2].StoreLength(), 0)
	assert.Equal(t, nodes[2].Get(key), "")
}

func testFollowerLeave(t *testing.T) {
	// Bring up a 5 nodes cluster
	nodes := newRaftCluster(t)
	addRaftNode(t, nodes)
	addRaftNode(t, nodes)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for failure test")

	resp, err := nodes[5].LeaveRaft(nodes[5].Ctx, &pb.NodeInfo{ID: nodes[5].ID})
	assert.NoError(t, err, "Error sending message to leave the raft")
	assert.Equal(t, resp.Error, "")

	// Propose a value
	nodes[1].Propose(nodes[1].Ctx, pair)

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// Value should be replicated on every node
	assert.Equal(t, nodes[1].StoreLength(), 1)
	assert.Equal(t, nodes[1].Get(key), string(value))
	assert.Equal(t, len(nodes[1].Cluster.Peers()), 4)

	assert.Equal(t, nodes[2].StoreLength(), 1)
	assert.Equal(t, nodes[2].Get(key), string(value))
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 4)

	assert.Equal(t, nodes[3].StoreLength(), 1)
	assert.Equal(t, nodes[3].Get(key), string(value))
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 4)

	assert.Equal(t, nodes[4].StoreLength(), 1)
	assert.Equal(t, nodes[4].Get(key), string(value))
	assert.Equal(t, len(nodes[4].Cluster.Peers()), 4)
}

func testLeaderLeave(t *testing.T) {
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	key := "foo"
	value := []byte("bar")

	pair, err := EncodePair(key, value)
	assert.NoError(t, err, "Can't encode key/value pair for failure test")

	// node 1 is the leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].ID)

	// Try to leave the raft
	resp, err := nodes[1].LeaveRaft(nodes[1].Ctx, &pb.NodeInfo{ID: nodes[1].ID})
	assert.NoError(t, err, "Error sending message to leave the raft")
	assert.Equal(t, resp.Error, "")

	// Wait for election tick
	time.Sleep(4 * time.Second)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[2].Leader(), nodes[3].Leader())

	// Propose a value
	nodes[2].Propose(nodes[2].Ctx, pair)

	// Wait heartbeat tick
	time.Sleep(1 * time.Second)

	// The value should be replicated on all remaining nodes
	assert.Equal(t, nodes[2].StoreLength(), 1)
	assert.Equal(t, nodes[2].Get(key), string(value))
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 2)

	assert.Equal(t, nodes[3].StoreLength(), 1)
	assert.Equal(t, nodes[3].Get(key), string(value))
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 2)
}

func testSnapshot(t *testing.T) {
	t.Skip()
}

func testRecoverSnapshot(t *testing.T) {
	t.Skip()
}
