package raft_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/docker/swarmkit/api"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
		values[i], err = raftutils.ProposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// None of the nodes should have snapshot files yet
	for _, node := range nodes {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
	values[3], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[3])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should now have a snapshot file
	for nodeID, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[4].StateDir, "snap"))
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
		values[i], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[i])
		assert.NoError(t, err, "failed to propose value")
	}

	// All nodes should have a snapshot under a *different* name
	for nodeID, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
		values[i], err = raftutils.ProposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// Take down node 3
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Propose a 4th value before the snapshot
	values[3], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[3])
	assert.NoError(t, err, "failed to propose value")

	// Remaining nodes shouldn't have snapshot files yet
	for _, node := range []*raftutils.TestNode{nodes[1], nodes[2]} {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
	values[4], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[4])
	require.NoError(t, err)

	// Add another node to the cluster
	nodes[5] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, tc)
	raftutils.WaitForCluster(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2], 4: nodes[4], 5: nodes[5]})

	// New node should get a copy of the snapshot
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d on new node", len(dirents))
		}
		return nil
	}))

	dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap"))
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
	values[5], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[5])
	require.NoError(t, err)

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:6], values[:6])

	// Restart node 3 again. It should load the snapshot.
	nodes[3].Server.Stop()
	nodes[3].Shutdown()
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	assert.Len(t, nodes[3].GetMemberlist(), 5)
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[3].GetMemberlist()))
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs[:6], values[:6])

	// Propose again. Just to check consensus after this latest restart.
	values[6], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[6])
	require.NoError(t, err)
	raftutils.CheckValuesOnNodes(t, clockSource, nodes, nodeIDs, values)
}
