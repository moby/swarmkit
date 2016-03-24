package state

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
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

func getTermAndLeader(n *Node) *leaderStatus {
	status := n.Status()
	return &leaderStatus{leader: status.Lead, term: status.Term}
}

// waitForCluster waits until leader will be one of specified nodes
func waitForCluster(t *testing.T, nodes map[uint64]*Node) {
	err := pollFunc(func() bool {
		var prev *leaderStatus
		for _, n := range nodes {
			if prev == nil {
				prev = getTermAndLeader(n)
				if _, ok := nodes[prev.leader]; !ok {
					return false
				}
				continue
			}
			cur := getTermAndLeader(n)
			if _, ok := nodes[cur.leader]; !ok {
				return false
			}
			if cur.leader != prev.leader || cur.term != prev.term {
				return false
			}
		}
		return true
	})
	require.NoError(t, err)
}

// waitForPeerNumber waits until peers in cluster converge to specified number
func waitForPeerNumber(t *testing.T, nodes map[uint64]*Node, count int) {
	assert.NoError(t, pollFunc(func() bool {
		for _, n := range nodes {
			if len(n.Cluster.Peers()) != count {
				return false
			}
		}
		return true
	}))
}

func newInitNode(t *testing.T, id uint64) *Node {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		ID:           id,
		Addr:         l.Addr().String(),
		Config:       cfg,
		StateDir:     stateDir,
		TickInterval: testTick,
	}
	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Listener = l
	n.Server = s

	err = n.Campaign(n.Ctx)
	require.NoError(t, err, "can't campaign to be the leader")
	n.Start()

	Register(s, n)

	done := make(chan error)
	go func() {
		done <- s.Serve(l)
	}()
	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, <-done)
	}()

	return n
}

func newJoinNode(t *testing.T, id uint64, join string) *Node {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		ID:           id,
		Addr:         l.Addr().String(),
		Config:       cfg,
		StateDir:     stateDir,
		TickInterval: testTick,
	}

	n, err := NewNode(context.Background(), newNodeOpts, nil)
	require.NoError(t, err, "can't create raft node")
	n.Listener = l
	n.Server = s

	n.Start()

	c, err := GetRaftClient(join, 500*time.Millisecond)
	assert.NoError(t, err, "can't initiate connection with existing raft")

	resp, err := c.Join(n.Ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: id, Addr: l.Addr().String()},
	})
	require.NoError(t, err, "can't join existing Raft")

	err = n.RegisterNodes(resp.Members)
	require.NoError(t, err, "can't add nodes to the local cluster list")

	Register(s, n)

	done := make(chan error)
	go func() {
		done <- s.Serve(l)
	}()
	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, <-done)
	}()
	return n
}

func newRaftCluster(t *testing.T) map[uint64]*Node {
	nodes := make(map[uint64]*Node)
	nodes[1] = newInitNode(t, 1)
	addRaftNode(t, nodes)
	addRaftNode(t, nodes)
	return nodes
}

func addRaftNode(t *testing.T, nodes map[uint64]*Node) {
	n := uint64(len(nodes) + 1)
	nodes[n] = newJoinNode(t, uint64(n), nodes[1].Listener.Addr().String())
	waitForCluster(t, nodes)
}

func teardownCluster(t *testing.T, nodes map[uint64]*Node) {
	for _, node := range nodes {
		shutdownNode(node)
	}
}

func removeNode(nodes map[string]*Node, node string) {
	shutdownNode(nodes[node])
	delete(nodes, node)
}

func shutdownNode(node *Node) {
	node.Server.Stop()
	node.Shutdown()
	os.RemoveAll(node.stateDir)
}

func TestRaftBootstrap(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.Equal(t, len(nodes[1].Cluster.Peers()), 3)
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 3)
}

func TestRaftLeader(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].ID)
}

func proposeValue(t *testing.T, raftNode *Node, nodeID ...string) (*api.Node, error) {
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

func checkValue(t *testing.T, raftNode *Node, createdNode *api.Node) {
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

func checkNoValue(t *testing.T, raftNode *Node) {
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

	newCluster := map[uint64]*Node{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for the re-election to occur
	waitForCluster(t, newCluster)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].ID)

	// Ensure that node 2 and node 3 have the same leader
	assert.Equal(t, nodes[3].Leader(), nodes[2].Leader())

	// Propose a value
	value, err := proposeValue(t, nodes[2])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[3], value)
	assert.Equal(t, len(nodes[3].Cluster.Peers()), 3)
}

func TestRaftFollowerDown(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Stop node 3
	nodes[3].Stop()

	// Leader should still be 1
	assert.True(t, nodes[1].IsLeader(), "node 1 is not a leader anymore")
	assert.Equal(t, nodes[2].Leader(), nodes[1].ID)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, nodes[1], value)
	assert.Equal(t, len(nodes[1].Cluster.Peers()), 3)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)
	assert.Equal(t, len(nodes[2].Cluster.Peers()), 3)
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
	resp, err := nodes[5].Leave(nodes[5].Ctx, &api.LeaveRequest{Node: &api.RaftNode{ID: nodes[5].ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	delete(nodes, 5)

	waitForPeerNumber(t, nodes, 4)

	// Propose a value
	value, err := proposeValue(t, nodes[1])
	assert.NoError(t, err, "failed to propose value")

	// Value should be replicated on every node
	checkValue(t, nodes[1], value)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[2], value)

	checkValue(t, nodes[3], value)

	checkValue(t, nodes[4], value)
}

func TestRaftLeaderLeave(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// node 1 is the leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].ID)

	// Try to leave the raft
	resp, err := nodes[1].Leave(nodes[1].Ctx, &api.LeaveRequest{Node: &api.RaftNode{ID: nodes[1].ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	newCluster := map[uint64]*Node{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for election tick
	waitForCluster(t, newCluster)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[2].Leader(), nodes[3].Leader())

	leader := nodes[2].Leader()
	follower := uint64(2)
	if leader == 2 {
		follower = 3
	}

	// Propose a value
	value, err := proposeValue(t, nodes[leader])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	checkValue(t, nodes[leader], value)
	assert.Equal(t, len(nodes[leader].Cluster.Peers()), 2)

	time.Sleep(500 * time.Millisecond)

	checkValue(t, nodes[follower], value)
	assert.Equal(t, len(nodes[follower].Cluster.Peers()), 2)
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
		assert.Equal(t, len(node.Cluster.Peers()), 4)
	}
}

func TestRaftSnapshot(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	// Override the interval between snapshots
	for _, node := range nodes {
		// Note that the cluster setup appears to involve 5 messages.
		atomic.StoreUint64(&node.snapshotInterval, 9)
		atomic.StoreUint64(&node.logEntriesForSlowFollowers, 0)
	}

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
