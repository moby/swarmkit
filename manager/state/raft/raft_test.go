package raft_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/coreos/etcd/pkg/fileutil"
	"github.com/coreos/etcd/wal"
	"github.com/docker/swarmkit/api"
	cautils "github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/state/raft"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	DefaultProposalTime = 10 * time.Second
	ShortProposalTime   = 1 * time.Second
)

var tc *cautils.TestCA

func TestMain(m *testing.M) {
	tc = cautils.NewTestCA(nil)

	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	logrus.SetOutput(ioutil.Discard)

	// Set a smaller segment size so we don't incur cost preallocating
	// space on old filesystems like HFS+.
	wal.SegmentSizeBytes = 64 * 1024

	res := m.Run()
	tc.Stop()
	os.Exit(res)
}

func TestRaftBootstrap(t *testing.T) {
	t.Parallel()

	nodes, _ := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	assert.Len(t, nodes[1].GetMemberlist(), 3)
	assert.Len(t, nodes[2].GetMemberlist(), 3)
	assert.Len(t, nodes[3].GetMemberlist(), 3)
}

func TestRaftJoinTwice(t *testing.T) {
	t.Parallel()

	nodes, _ := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Node 3 tries to join again
	// Use gRPC instead of calling handler directly because of
	// authorization check.
	client, err := nodes[3].ConnectToMember(nodes[1].Address, 10*time.Second)
	assert.NoError(t, err)
	raftClient := api.NewRaftMembershipClient(client.Conn)
	defer client.Conn.Close()
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	_, err = raftClient.Join(ctx, &api.JoinRequest{})
	assert.Error(t, err, "expected error on duplicate Join")
	assert.Equal(t, grpc.Code(err), codes.AlreadyExists)
	assert.Equal(t, grpc.ErrorDesc(err), "a raft member with this node ID already exists")
}

func TestRaftLeader(t *testing.T) {
	t.Parallel()

	nodes, _ := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	assert.True(t, nodes[1].IsLeader(), "error: node 1 is not the Leader")

	// nodes should all have the same leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[2].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[3].Leader(), nodes[1].Config.ID)
}

func TestRaftLeaderDown(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Stop node 1
	nodes[1].ShutdownRaft()

	newCluster := map[uint64]*raftutils.TestNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for the re-election to occur
	raftutils.WaitForCluster(t, clockSource, newCluster)

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].Config.ID)

	// Ensure that node 2 and node 3 have the same leader
	assert.Equal(t, nodes[3].Leader(), nodes[2].Leader())

	// Find the leader node and a follower node
	var (
		leaderNode   *raftutils.TestNode
		followerNode *raftutils.TestNode
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
	value, err := raftutils.ProposeValue(t, leaderNode, DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	raftutils.CheckValue(t, clockSource, leaderNode, value)
	assert.Len(t, leaderNode.GetMemberlist(), 3)

	raftutils.CheckValue(t, clockSource, followerNode, value)
	assert.Len(t, followerNode.GetMemberlist(), 3)
}

func TestRaftFollowerDown(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Stop node 3
	nodes[3].ShutdownRaft()

	// Leader should still be 1
	assert.True(t, nodes[1].IsLeader(), "node 1 is not a leader anymore")
	assert.Equal(t, nodes[2].Leader(), nodes[1].Config.ID)

	// Propose a value
	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	raftutils.CheckValue(t, clockSource, nodes[1], value)
	assert.Len(t, nodes[1].GetMemberlist(), 3)

	raftutils.CheckValue(t, clockSource, nodes[2], value)
	assert.Len(t, nodes[2].GetMemberlist(), 3)
}

func TestRaftLogReplication(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Propose a value
	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	raftutils.CheckValue(t, clockSource, nodes[1], value)
	raftutils.CheckValue(t, clockSource, nodes[2], value)
	raftutils.CheckValue(t, clockSource, nodes[3], value)
}

func TestRaftLogReplicationWithoutLeader(t *testing.T) {
	t.Parallel()
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Stop the leader
	nodes[1].ShutdownRaft()

	// Propose a value
	_, err := raftutils.ProposeValue(t, nodes[2], DefaultProposalTime)
	assert.Error(t, err)

	// No value should be replicated in the store in the absence of the leader
	raftutils.CheckNoValue(t, clockSource, nodes[2])
	raftutils.CheckNoValue(t, clockSource, nodes[3])
}

func TestRaftQuorumFailure(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Lose a majority
	for i := uint64(3); i <= 5; i++ {
		nodes[i].Server.Stop()
		nodes[i].ShutdownRaft()
	}

	// Propose a value
	_, err := raftutils.ProposeValue(t, nodes[1], ShortProposalTime)
	assert.Error(t, err)

	// The value should not be replicated, we have no majority
	raftutils.CheckNoValue(t, clockSource, nodes[2])
	raftutils.CheckNoValue(t, clockSource, nodes[1])
}

func TestRaftQuorumRecovery(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Lose a majority
	for i := uint64(1); i <= 3; i++ {
		nodes[i].Server.Stop()
		nodes[i].ShutdownRaft()
	}

	raftutils.AdvanceTicks(clockSource, 5)

	// Restore the majority by restarting node 3
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)

	raftutils.ShutdownNode(nodes[1])
	delete(nodes, 1)
	raftutils.ShutdownNode(nodes[2])
	delete(nodes, 2)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Propose a value
	value, err := raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime)
	assert.NoError(t, err)

	for _, node := range nodes {
		raftutils.CheckValue(t, clockSource, node, value)
	}
}

func TestRaftFollowerLeave(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Node 5 leaves the cluster
	// Use gRPC instead of calling handler directly because of
	// authorization check.
	client, err := nodes[1].ConnectToMember(nodes[1].Address, 10*time.Second)
	assert.NoError(t, err)
	raftClient := api.NewRaftMembershipClient(client.Conn)
	defer client.Conn.Close()
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := raftClient.Leave(ctx, &api.LeaveRequest{Node: &api.RaftMember{RaftID: nodes[5].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	raftutils.ShutdownNode(nodes[5])
	delete(nodes, 5)

	raftutils.WaitForPeerNumber(t, clockSource, nodes, 4)

	// Propose a value
	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// Value should be replicated on every node
	raftutils.CheckValue(t, clockSource, nodes[1], value)
	assert.Len(t, nodes[1].GetMemberlist(), 4)

	raftutils.CheckValue(t, clockSource, nodes[2], value)
	assert.Len(t, nodes[2].GetMemberlist(), 4)

	raftutils.CheckValue(t, clockSource, nodes[3], value)
	assert.Len(t, nodes[3].GetMemberlist(), 4)

	raftutils.CheckValue(t, clockSource, nodes[4], value)
	assert.Len(t, nodes[4].GetMemberlist(), 4)
}

func TestRaftLeaderLeave(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// node 1 is the leader
	assert.Equal(t, nodes[1].Leader(), nodes[1].Config.ID)

	// Try to leave the raft
	// Use gRPC instead of calling handler directly because of
	// authorization check.
	client, err := nodes[1].ConnectToMember(nodes[1].Address, 10*time.Second)
	assert.NoError(t, err)
	defer client.Conn.Close()
	raftClient := api.NewRaftMembershipClient(client.Conn)
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	resp, err := raftClient.Leave(ctx, &api.LeaveRequest{Node: &api.RaftMember{RaftID: nodes[1].Config.ID}})
	assert.NoError(t, err, "error sending message to leave the raft")
	assert.NotNil(t, resp, "leave response message is nil")

	newCluster := map[uint64]*raftutils.TestNode{
		2: nodes[2],
		3: nodes[3],
	}
	// Wait for election tick
	raftutils.WaitForCluster(t, clockSource, newCluster)

	// Node1's state should be cleared
	require.False(t, fileutil.Exist(filepath.Join(nodes[1].StateDir, "snap-v3-encrypted")))
	require.False(t, fileutil.Exist(filepath.Join(nodes[1].StateDir, "wal-v3-encrypted")))
	require.Equal(t, raft.EncryptionKeys{}, nodes[1].KeyRotator.GetKeys())

	// Leader should not be 1
	assert.NotEqual(t, nodes[2].Leader(), nodes[1].Config.ID)
	assert.Equal(t, nodes[2].Leader(), nodes[3].Leader())

	leader := nodes[2].Leader()

	// Find the leader node and a follower node
	var (
		leaderNode   *raftutils.TestNode
		followerNode *raftutils.TestNode
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
	value, err := raftutils.ProposeValue(t, leaderNode, DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on all remaining nodes
	raftutils.CheckValue(t, clockSource, leaderNode, value)
	assert.Len(t, leaderNode.GetMemberlist(), 2)

	raftutils.CheckValue(t, clockSource, followerNode, value)
	assert.Len(t, followerNode.GetMemberlist(), 2)

	raftutils.TeardownCluster(t, newCluster)
}

func TestRaftNewNodeGetsData(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Propose a value
	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// Add a new node
	raftutils.AddRaftNode(t, clockSource, nodes, tc)

	time.Sleep(500 * time.Millisecond)

	// Value should be replicated on every node
	for _, node := range nodes {
		raftutils.CheckValue(t, clockSource, node, value)
		assert.Len(t, node.GetMemberlist(), 4)
	}
}

func TestRaftRejoin(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	ids := []string{"id1", "id2"}

	// Propose a value
	values := make([]*api.Node, 2)
	var err error
	values[0], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, ids[0])
	assert.NoError(t, err, "failed to propose value")

	// The value should be replicated on node 3
	raftutils.CheckValue(t, clockSource, nodes[3], values[0])
	assert.Len(t, nodes[3].GetMemberlist(), 3)

	// Stop node 3
	nodes[3].Server.Stop()
	nodes[3].ShutdownRaft()

	// Propose another value
	values[1], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, ids[1])
	assert.NoError(t, err, "failed to propose value")

	// Nodes 1 and 2 should have the new value
	raftutils.CheckValuesOnNodes(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2]}, ids, values)

	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Node 3 should have all values, including the one proposed while
	// it was unavailable.
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, ids, values)
}

func testRaftRestartCluster(t *testing.T, stagger bool) {
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Propose a value
	values := make([]*api.Node, 2)
	var err error
	values[0], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, "id1")
	assert.NoError(t, err, "failed to propose value")

	// Stop all nodes
	for _, node := range nodes {
		node.Server.Stop()
		node.ShutdownRaft()
	}

	raftutils.AdvanceTicks(clockSource, 5)

	// Restart all nodes
	i := 0
	for k, node := range nodes {
		if stagger && i != 0 {
			raftutils.AdvanceTicks(clockSource, 1)
		}
		nodes[k] = raftutils.RestartNode(t, clockSource, node, false)
		i++
	}
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Propose another value
	values[1], err = raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, "id2")
	assert.NoError(t, err, "failed to propose value")

	for _, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			var err error
			node.MemoryStore().View(func(tx store.ReadTx) {
				var allNodes []*api.Node
				allNodes, err = store.FindNodes(tx, store.All)
				if err != nil {
					return
				}
				if len(allNodes) != 2 {
					err = fmt.Errorf("expected 2 nodes, got %d", len(allNodes))
					return
				}

				for i, nodeID := range []string{"id1", "id2"} {
					n := store.GetNode(tx, nodeID)
					if !reflect.DeepEqual(n, values[i]) {
						err = fmt.Errorf("node %s did not match expected value", nodeID)
						return
					}
				}
			})
			return err
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

func TestRaftWipedState(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Stop node 3
	nodes[3].Server.Stop()
	nodes[3].ShutdownRaft()

	// Remove its state
	os.RemoveAll(nodes[3].StateDir)

	raftutils.AdvanceTicks(clockSource, 5)

	// Restart node 3
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)

	// Make sure this doesn't panic.
	raftutils.PollFuncWithTimeout(clockSource, func() error { return errors.New("keep the poll going") }, time.Second)
}

func TestRaftForceNewCluster(t *testing.T) {
	t.Parallel()

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Propose a value
	values := make([]*api.Node, 2)
	var err error
	values[0], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, "id1")
	assert.NoError(t, err, "failed to propose value")

	// The memberlist should contain 3 members on each node
	for i := 1; i <= 3; i++ {
		assert.Len(t, nodes[uint64(i)].GetMemberlist(), 3)
	}

	// Stop all nodes
	for _, node := range nodes {
		node.Server.Stop()
		node.ShutdownRaft()
	}

	raftutils.AdvanceTicks(clockSource, 5)

	toClean := map[uint64]*raftutils.TestNode{
		2: nodes[2],
		3: nodes[3],
	}
	raftutils.TeardownCluster(t, toClean)
	delete(nodes, 2)
	delete(nodes, 3)

	// Only restart the first node with force-new-cluster option
	nodes[1] = raftutils.RestartNode(t, clockSource, nodes[1], true)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// The memberlist should contain only one node (self)
	assert.Len(t, nodes[1].GetMemberlist(), 1)

	// Add 2 more members
	nodes[2] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, tc)
	raftutils.WaitForCluster(t, clockSource, nodes)

	nodes[3] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, tc)
	raftutils.WaitForCluster(t, clockSource, nodes)

	newCluster := map[uint64]*raftutils.TestNode{
		1: nodes[1],
		2: nodes[2],
		3: nodes[3],
	}
	defer raftutils.TeardownCluster(t, newCluster)

	// The memberlist should contain 3 members on each node
	for i := 1; i <= 3; i++ {
		assert.Len(t, nodes[uint64(i)].GetMemberlist(), 3)
	}

	// Propose another value
	values[1], err = raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, "id2")
	assert.NoError(t, err, "failed to propose value")

	for _, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			var err error
			node.MemoryStore().View(func(tx store.ReadTx) {
				var allNodes []*api.Node
				allNodes, err = store.FindNodes(tx, store.All)
				if err != nil {
					return
				}
				if len(allNodes) != 2 {
					err = fmt.Errorf("expected 2 nodes, got %d", len(allNodes))
					return
				}

				for i, nodeID := range []string{"id1", "id2"} {
					n := store.GetNode(tx, nodeID)
					if !reflect.DeepEqual(n, values[i]) {
						err = fmt.Errorf("node %s did not match expected value", nodeID)
						return
					}
				}
			})
			return err
		}))
	}
}

func TestRaftUnreachableNode(t *testing.T) {
	t.Parallel()

	nodes := make(map[uint64]*raftutils.TestNode)
	var clockSource *fakeclock.FakeClock
	nodes[1], clockSource = raftutils.NewInitNode(t, tc, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Add a new node
	nodes[2] = raftutils.NewNode(t, clockSource, tc, raft.NodeOptions{JoinAddr: nodes[1].Address})

	err := nodes[2].JoinAndStart(ctx)
	require.NoError(t, err, "can't join cluster")

	go nodes[2].Run(ctx)

	// Stop the Raft server of second node on purpose after joining
	nodes[2].Server.Stop()
	nodes[2].Listener.Close()

	raftutils.AdvanceTicks(clockSource, 5)
	time.Sleep(100 * time.Millisecond)

	wrappedListener := raftutils.RecycleWrappedListener(nodes[2].Listener)
	securityConfig := nodes[2].SecurityConfig
	serverOpts := []grpc.ServerOption{grpc.Creds(securityConfig.ServerTLSCreds)}
	s := grpc.NewServer(serverOpts...)

	nodes[2].Server = s
	raft.Register(s, nodes[2].Node)

	go func() {
		// After stopping, we should receive an error from Serve
		assert.Error(t, s.Serve(wrappedListener))
	}()

	raftutils.WaitForCluster(t, clockSource, nodes)
	defer raftutils.TeardownCluster(t, nodes)

	// Propose a value
	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime)
	assert.NoError(t, err, "failed to propose value")

	// All nodes should have the value in the physical store
	raftutils.CheckValue(t, clockSource, nodes[1], value)
	raftutils.CheckValue(t, clockSource, nodes[2], value)
}

func TestRaftJoinWithIncorrectAddress(t *testing.T) {
	t.Parallel()

	nodes := make(map[uint64]*raftutils.TestNode)
	var clockSource *fakeclock.FakeClock
	nodes[1], clockSource = raftutils.NewInitNode(t, tc, nil)
	defer raftutils.ShutdownNode(nodes[1])

	// Try joining a new node with an incorrect address
	n := raftutils.NewNode(t, clockSource, tc, raft.NodeOptions{JoinAddr: nodes[1].Address, Addr: "1.2.3.4:1234"})
	defer raftutils.CleanupNonRunningNode(n)

	err := n.JoinAndStart(context.Background())
	assert.NotNil(t, err)
	assert.Contains(t, grpc.ErrorDesc(err), "could not connect to prospective new cluster member using its advertised address")

	// Check if first node still has only itself registered in the memberlist
	assert.Len(t, nodes[1].GetMemberlist(), 1)
}

func TestStress(t *testing.T) {
	t.Parallel()

	// Bring up a 5 nodes cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// number of nodes that are running
	nup := len(nodes)
	// record of nodes that are down
	idleNodes := map[int]struct{}{}
	// record of ids that proposed successfully or time-out
	pIDs := []string{}

	leader := -1
	for iters := 0; iters < 1000; iters++ {
		// keep proposing new values and killing leader
		for i := 1; i <= 5; i++ {
			if nodes[uint64(i)] != nil {
				id := strconv.Itoa(iters)
				_, err := raftutils.ProposeValue(t, nodes[uint64(i)], ShortProposalTime, id)

				if err == nil {
					pIDs = append(pIDs, id)
					// if propose successfully, at least there are 3 running nodes
					assert.True(t, nup >= 3)
					// only leader can propose value
					assert.True(t, leader == i || leader == -1)
					// update leader
					leader = i
					break
				} else {
					// though ProposeValue returned an error, we still record this value,
					// for it may be proposed successfully and stored in Raft some time later
					pIDs = append(pIDs, id)
				}
			}
		}

		if rand.Intn(100) < 10 {
			// increase clock to make potential election finish quickly
			clockSource.Increment(200 * time.Millisecond)
			time.Sleep(10 * time.Millisecond)
		} else {
			ms := rand.Intn(10)
			clockSource.Increment(time.Duration(ms) * time.Millisecond)
		}

		if leader != -1 {
			// if propose successfully, try to kill a node in random
			s := rand.Intn(5) + 1
			if _, ok := idleNodes[s]; !ok {
				id := uint64(s)
				nodes[id].Server.Stop()
				nodes[id].ShutdownRaft()
				idleNodes[s] = struct{}{}
				nup -= 1
				if s == leader {
					// leader is killed
					leader = -1
				}
			}
		}

		if nup < 3 {
			// if quorum is lost, try to bring back a node
			s := rand.Intn(5) + 1
			if _, ok := idleNodes[s]; ok {
				id := uint64(s)
				nodes[id] = raftutils.RestartNode(t, clockSource, nodes[id], false)
				delete(idleNodes, s)
				nup++
			}
		}
	}

	// bring back all nodes and propose the final value
	for i := range idleNodes {
		id := uint64(i)
		nodes[id] = raftutils.RestartNode(t, clockSource, nodes[id], false)
	}
	raftutils.WaitForCluster(t, clockSource, nodes)
	id := strconv.Itoa(1000)
	val, err := raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, id)
	assert.NoError(t, err, "failed to propose value")
	pIDs = append(pIDs, id)

	// increase clock to make cluster stable
	time.Sleep(500 * time.Millisecond)
	clockSource.Increment(500 * time.Millisecond)

	ids, values := raftutils.GetAllValuesOnNode(t, clockSource, nodes[1])

	// since cluster is stable, final value must be in the raft store
	find := false
	for _, value := range values {
		if reflect.DeepEqual(value, val) {
			find = true
			break
		}
	}
	assert.True(t, find)

	// all nodes must have the same value
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, ids, values)

	// ids should be a subset of pIDs
	for _, id := range ids {
		find = false
		for _, pid := range pIDs {
			if id == pid {
				find = true
				break
			}
		}
		assert.True(t, find)
	}
}
