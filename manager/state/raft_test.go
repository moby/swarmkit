package state

import (
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"

	"github.com/coreos/etcd/raft"
	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
)

const testTick = 50 * time.Millisecond

var (
	raftLogger = &raft.DefaultLogger{Logger: log.New(ioutil.Discard, "", 0)}
)

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
}

func pollNodeFunc(nodes map[uint64]*Node, f func(*Node) bool) error {
	var tickers []*time.Ticker
	var wg sync.WaitGroup
	for _, n := range nodes {
		wg.Add(1)
		if f(n) {
			wg.Done()
			continue
		}
		tick := time.NewTicker(testTick)
		tickers = append(tickers, tick)
		go func(n *Node) {
			for range tick.C {
				if f(n) {
					break
				}
			}
			wg.Done()
		}(n)
	}
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-time.After(10 * time.Second):
		for _, tick := range tickers {
			tick.Stop()
		}
		return fmt.Errorf("polling failed")
	}
}

func getTermAndLeader(n *Node) (uint64, uint64) {
	status := n.Status()
	return status.Lead, status.Term
}

// waitForCluster waits until leader will be one of specified nodes
func waitForCluster(t *testing.T, nodes map[uint64]*Node) {
	assert.NoError(t, pollNodeFunc(nodes, func(n *Node) bool {
		l, t := getTermAndLeader(n)
		if _, ok := nodes[l]; !ok {
			return false
		}
		for _, node := range nodes {
			rl, rt := getTermAndLeader(node)
			if rl != l || rt != t {
				return false
			}
		}
		return true
	}))
}

// waitForPeerNumber waits until peers in cluster converge to specified number
func waitForPeerNumber(t *testing.T, nodes map[uint64]*Node, count int) {
	assert.NoError(t, pollNodeFunc(nodes, func(n *Node) bool {
		return len(n.Cluster.Peers()) == count
	}))
}

func newInitNode(t *testing.T, id uint64) *Node {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err, "can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		ID:           id,
		Addr:         l.Addr().String(),
		Config:       cfg,
		StateDir:     stateDir,
		TickInterval: testTick,
	}
	n, err := NewNode(context.Background(), newNodeOpts)
	assert.NoError(t, err, "can't create raft node")
	n.Listener = l
	n.Server = s

	err = n.Campaign(n.Ctx)
	assert.NoError(t, err, "can't campaign to be the leader")
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
	assert.NoError(t, err, "can't bind to raft service port")
	s := grpc.NewServer()

	cfg := DefaultNodeConfig()
	cfg.Logger = raftLogger

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err, "can't create temporary state directory")

	newNodeOpts := NewNodeOptions{
		ID:           id,
		Addr:         l.Addr().String(),
		Config:       cfg,
		StateDir:     stateDir,
		TickInterval: testTick,
	}

	n, err := NewNode(context.Background(), newNodeOpts)
	assert.NoError(t, err, "can't create raft node")
	n.Listener = l
	n.Server = s

	n.Start()

	c, err := GetRaftClient(join, 100*time.Millisecond)
	assert.NoError(t, err, "can't initiate connection with existing raft")

	resp, err := c.Join(n.Ctx, &api.JoinRequest{
		Node: &api.RaftNode{ID: id, Addr: l.Addr().String()},
	})
	assert.NoError(t, err, "can't join existing Raft")

	err = n.RegisterNodes(resp.Members)
	assert.NoError(t, err, "can't add nodes to the local cluster list")

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
	nodes[2] = newJoinNode(t, 2, nodes[1].Listener.Addr().String())
	nodes[3] = newJoinNode(t, 3, nodes[1].Listener.Addr().String())
	waitForCluster(t, nodes)
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

func TestLeader(t *testing.T) {
	t.Parallel()

	nodes := newRaftCluster(t)
	defer teardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].ID)
}

func proposeValue(t *testing.T, raftNode *Node) (*api.Node, error) {
	node := &api.Node{
		ID: "id1",
		Spec: &api.NodeSpec{
			Meta: api.Meta{
				Name: "name1",
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

	err := raftNode.ProposeValue(context.Background(), storeActions)
	if err != nil {
		return nil, err
	}

	err = raftNode.memoryStore.applyStoreActions(storeActions)
	assert.NoError(t, err, "error applying actions")

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

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

	time.Sleep(100 * time.Millisecond)

	// Value should be replicated on every node
	for _, node := range nodes {
		checkValue(t, node, value)
		assert.Equal(t, len(node.Cluster.Peers()), 4)
	}
}

func TestRaftSnapshot(t *testing.T) {
	t.Skip()
}

func TestRaftRecoverSnapshot(t *testing.T) {
	t.Skip()
}
