package raft_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/pivotal-golang/clock/fakeclock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func TestRaftSnapshot(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc, &api.RaftConfig{SnapshotInterval: 9, LogEntriesForSlowFollowers: 0})
	defer raftutils.TeardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5", "id6", "id7", "id8", "id9", "id10", "id11", "id12"}
	values := make([]*api.Node, len(nodeIDs))
	snapshotFilenames := make(map[uint64]string, 4)

	// Propose 3 values
	var err error
	for i, nodeID := range nodeIDs[:3] {
		values[i], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// None of the nodes should have snapshot files yet
	for _, node := range nodes {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap-v3"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Check all nodes have all the data.
	// This also acts as a synchronization point so that the next value we
	// propose will arrive as a separate message to the raft state machine,
	// and it is guaranteed to have the right cluster settings when
	// deciding whether to create a new snapshot.
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:3], values)

	// Propose a 4th value
	values[3], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeIDs[3])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should now have a snapshot file
	for nodeID, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap-v3"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
			}
			snapshotFilenames[nodeID] = dirents[0].Name()
			return nil
		}))
	}

	// Add a node to the cluster
	raftutils.AddRaftNode(t, clockSource, nodes, tc)

	// It should get a copy of the snapshot
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[4].StateDir, "snap-v3"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d on new node", len(dirents))
		}
		snapshotFilenames[4] = dirents[0].Name()
		return nil
	}))

	// It should know about the other nodes
	stripMembers := func(memberList map[uint64]*api.RaftMember) map[uint64]*api.RaftMember {
		raftNodes := make(map[uint64]*api.RaftMember)
		for k, v := range memberList {
			raftNodes[k] = &api.RaftMember{
				RaftID: v.RaftID,
				Addr:   v.Addr,
			}
		}
		return raftNodes
	}
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[4].GetMemberlist()))

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:4], values)

	// Propose more values to provoke a second snapshot
	for i := 4; i != len(nodeIDs); i++ {
		values[i], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeIDs[i])
		assert.NoError(t, err, "failed to propose value")
	}

	// All nodes should have a snapshot under a *different* name
	for nodeID, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap-v3"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d on node %d", len(dirents), nodeID)
			}
			if dirents[0].Name() == snapshotFilenames[nodeID] {
				return fmt.Errorf("snapshot %s did not get replaced", snapshotFilenames[nodeID])
			}
			return nil
		}))
	}

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs, values)
}

func TestRaftSnapshotRestart(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc, &api.RaftConfig{SnapshotInterval: 10, LogEntriesForSlowFollowers: 0})
	defer raftutils.TeardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5", "id6", "id7"}
	values := make([]*api.Node, len(nodeIDs))

	// Propose 3 values
	var err error
	for i, nodeID := range nodeIDs[:3] {
		values[i], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// Take down node 3
	nodes[3].Server.Stop()
	nodes[3].ShutdownRaft()

	// Propose a 4th value before the snapshot
	values[3], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeIDs[3])
	assert.NoError(t, err, "failed to propose value")

	// Remaining nodes shouldn't have snapshot files yet
	for _, node := range []*raftutils.TestNode{nodes[1], nodes[2]} {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap-v3"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Add a node to the cluster before the snapshot. This is the event
	// that triggers the snapshot.
	nodes[4] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, tc)
	raftutils.WaitForCluster(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2], 4: nodes[4]})

	// Remaining nodes should now have a snapshot file
	for nodeIdx, node := range []*raftutils.TestNode{nodes[1], nodes[2]} {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap-v3"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d on node %d", len(dirents), nodeIdx+1)
			}
			return nil
		}))
	}
	raftutils.CheckValuesOnNodes(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:4], values[:4])

	// Propose a 5th value
	values[4], err = raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, nodeIDs[4])
	require.NoError(t, err)

	// Add another node to the cluster
	nodes[5] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, tc)
	raftutils.WaitForCluster(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2], 4: nodes[4], 5: nodes[5]})

	// New node should get a copy of the snapshot
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap-v3"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d on new node", len(dirents))
		}
		return nil
	}))

	dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap-v3"))
	assert.NoError(t, err)
	assert.Len(t, dirents, 1)
	raftutils.CheckValuesOnNodes(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:5], values[:5])

	// It should know about the other nodes, including the one that was just added
	stripMembers := func(memberList map[uint64]*api.RaftMember) map[uint64]*api.RaftMember {
		raftNodes := make(map[uint64]*api.RaftMember)
		for k, v := range memberList {
			raftNodes[k] = &api.RaftMember{
				RaftID: v.RaftID,
				Addr:   v.Addr,
			}
		}
		return raftNodes
	}
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[4].GetMemberlist()))

	// Restart node 3
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Node 3 should know about other nodes, including the new one
	assert.Len(t, nodes[3].GetMemberlist(), 5)
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[3].GetMemberlist()))

	// Propose yet another value, to make sure the rejoined node is still
	// receiving new logs
	values[5], err = raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, nodeIDs[5])
	require.NoError(t, err)

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:6], values[:6])

	// Restart node 3 again. It should load the snapshot.
	nodes[3].Server.Stop()
	nodes[3].ShutdownRaft()
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	assert.Len(t, nodes[3].GetMemberlist(), 5)
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[3].GetMemberlist()))
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:6], values[:6])

	// Propose again. Just to check consensus after this latest restart.
	values[6], err = raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, nodeIDs[6])
	require.NoError(t, err)
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs, values)
}

func TestGCWAL(t *testing.T) {
	t.Parallel()

	// Additional log entries from cluster setup, leader election
	extraLogEntries := 5
	// Number of large entries to propose
	proposals := 8

	// Bring up a 3 node cluster
	nodes, clockSource := raftutils.NewRaftCluster(t, tc, &api.RaftConfig{SnapshotInterval: uint64(proposals + extraLogEntries), LogEntriesForSlowFollowers: 0})

	for i := 0; i != proposals; i++ {
		_, err := proposeLargeValue(t, nodes[1], DefaultProposalTime, fmt.Sprintf("id%d", i))
		assert.NoError(t, err, "failed to propose value")
	}

	time.Sleep(250 * time.Millisecond)

	// Snapshot should have been triggered just as the WAL rotated, so
	// both WAL files should be preserved
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[1].StateDir, "snap-v3"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}

		dirents, err = ioutil.ReadDir(filepath.Join(nodes[1].StateDir, "wal-v3"))
		if err != nil {
			return err
		}
		var walCount int
		for _, f := range dirents {
			if strings.HasSuffix(f.Name(), ".wal") {
				walCount++
			}
		}
		if walCount != 2 {
			return fmt.Errorf("expected 2 WAL files, found %d", walCount)
		}
		return nil
	}))

	raftutils.TeardownCluster(t, nodes)

	// Repeat this test, but trigger the snapshot after the WAL has rotated
	proposals++
	nodes, clockSource = raftutils.NewRaftCluster(t, tc, &api.RaftConfig{SnapshotInterval: uint64(proposals + extraLogEntries), LogEntriesForSlowFollowers: 0})
	defer raftutils.TeardownCluster(t, nodes)

	for i := 0; i != proposals; i++ {
		_, err := proposeLargeValue(t, nodes[1], DefaultProposalTime, fmt.Sprintf("id%d", i))
		assert.NoError(t, err, "failed to propose value")
	}

	time.Sleep(250 * time.Millisecond)

	// This time only one WAL file should be saved.
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[1].StateDir, "snap-v3"))
		if err != nil {
			return err
		}

		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}

		dirents, err = ioutil.ReadDir(filepath.Join(nodes[1].StateDir, "wal-v3"))
		if err != nil {
			return err
		}
		var walCount int
		for _, f := range dirents {
			if strings.HasSuffix(f.Name(), ".wal") {
				walCount++
			}
		}
		if walCount != 1 {
			return fmt.Errorf("expected 1 WAL file, found %d", walCount)
		}
		return nil
	}))

	// Restart the whole cluster
	for _, node := range nodes {
		node.Server.Stop()
		node.ShutdownRaft()
	}

	raftutils.AdvanceTicks(clockSource, 5)

	i := 0
	for k, node := range nodes {
		nodes[k] = raftutils.RestartNode(t, clockSource, node, false)
		i++
	}
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Is the data intact after restart?
	for _, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			var err error
			node.MemoryStore().View(func(tx store.ReadTx) {
				var allNodes []*api.Node
				allNodes, err = store.FindNodes(tx, store.All)
				if err != nil {
					return
				}
				if len(allNodes) != proposals {
					err = fmt.Errorf("expected %d nodes, got %d", proposals, len(allNodes))
					return
				}
			})
			return err
		}))
	}

	// It should still be possible to propose values
	_, err := raftutils.ProposeValue(t, raftutils.Leader(nodes), DefaultProposalTime, "newnode")
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
				if len(allNodes) != proposals+1 {
					err = fmt.Errorf("expected %d nodes, got %d", proposals, len(allNodes))
					return
				}
			})
			return err
		}))
	}
}

// proposeLargeValue proposes a 10kb value to a raft test cluster
func proposeLargeValue(t *testing.T, raftNode *raftutils.TestNode, time time.Duration, nodeID ...string) (*api.Node, error) {
	nodeIDStr := "id1"
	if len(nodeID) != 0 {
		nodeIDStr = nodeID[0]
	}
	a := make([]byte, 10000)
	for i := 0; i != len(a); i++ {
		a[i] = 'a'
	}
	node := &api.Node{
		ID: nodeIDStr,
		Spec: api.NodeSpec{
			Annotations: api.Annotations{
				Name: nodeIDStr,
				Labels: map[string]string{
					"largestring": string(a),
				},
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

	ctx, _ := context.WithTimeout(context.Background(), time)

	err := raftNode.ProposeValue(ctx, storeActions, func() {
		err := raftNode.MemoryStore().ApplyStoreActions(storeActions)
		assert.NoError(t, err, "error applying actions")
	})
	if err != nil {
		return nil, err
	}

	return node, nil
}

func TestMigrateWAL(t *testing.T) {
	nodes := make(map[uint64]*raftutils.TestNode)
	var clockSource *fakeclock.FakeClock

	nodes[1], clockSource = raftutils.NewInitNode(t, tc, nil)
	defer raftutils.TeardownCluster(t, nodes)

	value, err := raftutils.ProposeValue(t, nodes[1], DefaultProposalTime, "id1")
	assert.NoError(t, err, "failed to propose value")
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, []string{"id1"}, []*api.Node{value})

	nodes[1].Server.Stop()
	nodes[1].ShutdownRaft()

	// Move WAL directory so it looks like it was created by an old version
	require.NoError(t, os.Rename(filepath.Join(nodes[1].StateDir, "wal-v3"), filepath.Join(nodes[1].StateDir, "wal")))

	nodes[1] = raftutils.RestartNode(t, clockSource, nodes[1], false)
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, []string{"id1"}, []*api.Node{value})
}
