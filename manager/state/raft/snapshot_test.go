package raft_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/raft"
	raftutils "github.com/docker/swarm-v2/manager/state/raft/testutils"
	"github.com/stretchr/testify/assert"
)

func TestRaftSnapshot(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	var zero uint64
	nodes, clockSource := raftutils.NewRaftCluster(t, securityConfig, raft.NewNodeOptions{SnapshotInterval: 9, LogEntriesForSlowFollowers: &zero})
	defer raftutils.TeardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5"}
	values := make([]*api.Node, len(nodeIDs))

	// Propose 4 values
	var err error
	for i, nodeID := range nodeIDs[:4] {
		values[i], err = raftutils.ProposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// None of the nodes should have snapshot files yet
	for _, node := range nodes {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Propose a 5th value
	values[4], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[4])
	assert.NoError(t, err, "failed to propose value")

	// All nodes should now have a snapshot file
	for _, node := range nodes {
		assert.NoError(t, raftutils.PollFunc(func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
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
	raftutils.AddRaftNode(t, clockSource, nodes, securityConfig)

	// It should get a copy of the snapshot
	assert.NoError(t, raftutils.PollFunc(func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[4].StateDir, "snap"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}
		return nil
	}))

	// It should know about the other nodes
	stripMembers := func(memberList map[uint64]*api.Member) map[uint64]*api.Member {
		raftNodes := make(map[uint64]*api.Member)
		for k, v := range memberList {
			raftNodes[k] = &api.Member{
				ID:     v.ID,
				RaftID: v.RaftID,
				Addr:   v.Addr,
			}
		}
		return raftNodes
	}
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[4].GetMemberlist()))

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, nodes, nodeIDs, values)
}

func TestRaftSnapshotRestart(t *testing.T) {
	t.Parallel()

	// Bring up a 3 node cluster
	var zero uint64
	nodes, clockSource := raftutils.NewRaftCluster(t, securityConfig, raft.NewNodeOptions{SnapshotInterval: 10, LogEntriesForSlowFollowers: &zero})
	defer raftutils.TeardownCluster(t, nodes)

	nodeIDs := []string{"id1", "id2", "id3", "id4", "id5", "id6", "id7", "id8"}
	values := make([]*api.Node, len(nodeIDs))

	// Propose 4 values
	var err error
	for i, nodeID := range nodeIDs[:4] {
		values[i], err = raftutils.ProposeValue(t, nodes[1], nodeID)
		assert.NoError(t, err, "failed to propose value")
	}

	// Take down node 3
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Propose a 5th value before the snapshot
	values[4], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[4])
	assert.NoError(t, err, "failed to propose value")

	// Remaining nodes shouldn't have snapshot files yet
	for _, node := range []*raftutils.TestNode{nodes[1], nodes[2]} {
		dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
		assert.NoError(t, err)
		assert.Len(t, dirents, 0)
	}

	// Add a node to the cluster before the snapshot. This is the event
	// that triggers the snapshot.
	nodes[4] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, securityConfig)
	raftutils.WaitForCluster(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2], 4: nodes[4]})

	// Remaining nodes should now have a snapshot file
	for _, node := range []*raftutils.TestNode{nodes[1], nodes[2]} {
		assert.NoError(t, raftutils.PollFunc(func() error {
			dirents, err := ioutil.ReadDir(filepath.Join(node.StateDir, "snap"))
			if err != nil {
				return err
			}
			if len(dirents) != 1 {
				return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
			}
			return nil
		}))
	}
	raftutils.CheckValuesOnNodes(t, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:5], values[:5])

	// Propose a 6th value
	values[5], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[5])

	// Add another node to the cluster
	nodes[5] = raftutils.NewJoinNode(t, clockSource, nodes[1].Address, securityConfig)
	raftutils.WaitForCluster(t, clockSource, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2], 4: nodes[4], 5: nodes[5]})

	// New node should get a copy of the snapshot
	assert.NoError(t, raftutils.PollFunc(func() error {
		dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap"))
		if err != nil {
			return err
		}
		if len(dirents) != 1 {
			return fmt.Errorf("expected 1 snapshot, found %d", len(dirents))
		}
		return nil
	}))

	dirents, err := ioutil.ReadDir(filepath.Join(nodes[5].StateDir, "snap"))
	assert.NoError(t, err)
	assert.Len(t, dirents, 1)
	raftutils.CheckValuesOnNodes(t, map[uint64]*raftutils.TestNode{1: nodes[1], 2: nodes[2]}, nodeIDs[:6], values[:6])

	// It should know about the other nodes, including the one that was just added
	stripMembers := func(memberList map[uint64]*api.Member) map[uint64]*api.Member {
		raftNodes := make(map[uint64]*api.Member)
		for k, v := range memberList {
			raftNodes[k] = &api.Member{
				ID:     v.ID,
				RaftID: v.RaftID,
				Addr:   v.Addr,
			}
		}
		return raftNodes
	}
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[4].GetMemberlist()))

	// Restart node 3
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], securityConfig)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Node 3 should know about other nodes, including the new one
	assert.Len(t, nodes[3].GetMemberlist(), 5)
	assert.Equal(t, stripMembers(nodes[1].GetMemberlist()), stripMembers(nodes[3].GetMemberlist()))

	// Propose yet another value, to make sure the rejoined node is still
	// receiving new logs
	values[6], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[6])

	// All nodes should have all the data
	raftutils.CheckValuesOnNodes(t, nodes, nodeIDs[:7], values[:7])

	// Restart node 3 again. It should load the snapshot.
	nodes[3].Server.Stop()
	nodes[3].Shutdown()
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], securityConfig)
	raftutils.WaitForCluster(t, clockSource, nodes)

	assert.Len(t, nodes[3].GetMemberlist(), 5)
	assert.Equal(t, nodes[1].GetMemberlist(), nodes[3].GetMemberlist())
	raftutils.CheckValuesOnNodes(t, nodes, nodeIDs[:7], values[:7])

	// Propose again. Just to check consensus after this latest restart.
	values[7], err = raftutils.ProposeValue(t, nodes[1], nodeIDs[7])
	raftutils.CheckValuesOnNodes(t, nodes, nodeIDs, values)
}
