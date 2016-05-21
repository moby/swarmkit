package controlapi

import (
	"fmt"
	"io/ioutil"
	"log"
	"testing"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	raftutils "github.com/docker/swarm-v2/manager/state/raft/testutils"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc"
	"google.golang.org/grpc/grpclog"
)

var securityConfig *ca.SecurityConfig

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	logrus.SetOutput(ioutil.Discard)

	tc := testutils.NewTestCA(nil, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	securityConfig, _ = tc.NewNodeConfig(ca.ManagerRole)
}

func getMap(t *testing.T, members []*api.Member) map[uint64]*api.Member {
	m := make(map[uint64]*api.Member)
	for _, member := range members {
		m[member.RaftID] = member
	}
	return m
}

func TestListManagers(t *testing.T) {
	ts := newTestServer(t)

	nodes, clockSource := raftutils.NewRaftCluster(t, securityConfig)
	defer raftutils.TeardownCluster(t, nodes)

	// Assign one of the raft node to the test server
	ts.Server.raft = nodes[1].Node

	// There should be 3 reachable managers listed
	r, err := ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	members := getMap(t, r.Managers)
	assert.Equal(t, 3, len(ts.Server.raft.GetMemberlist()))
	assert.Equal(t, 3, len(r.Managers))

	// Node 1 should be the leader
	for i := 1; i <= 3; i++ {
		if i == 1 {
			assert.True(t, members[nodes[uint64(i)].Config.ID].Status.Leader)
			continue
		}
		assert.False(t, members[nodes[uint64(i)].Config.ID].Status.Leader)
	}

	// All nodes should be reachable
	for i := 1; i <= 3; i++ {
		assert.Equal(t, api.MemberStatus_REACHABLE, members[nodes[uint64(i)].Config.ID].Status.State)
	}

	// Add two more nodes to the cluster
	raftutils.AddRaftNode(t, clockSource, nodes, securityConfig)
	raftutils.AddRaftNode(t, clockSource, nodes, securityConfig)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// There should be 5 reachable managers listed
	r, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	members = getMap(t, r.Managers)
	assert.Equal(t, 5, len(ts.Server.raft.GetMemberlist()))
	assert.Equal(t, 5, len(r.Managers))
	for i := 1; i <= 5; i++ {
		assert.Equal(t, api.MemberStatus_REACHABLE, members[nodes[uint64(i)].Config.ID].Status.State)
	}

	// Stops 2 nodes
	nodes[4].Server.Stop()
	nodes[4].Shutdown()
	nodes[5].Server.Stop()
	nodes[5].Shutdown()

	// Node 4 and Node 5 should be listed as Unreachable
	assert.NoError(t, raftutils.PollFunc(func() error {
		r, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
		if err != nil {
			return err
		}

		members = getMap(t, r.Managers)

		if len(r.Managers) != 5 {
			return fmt.Errorf("expected 5 nodes, got %d", len(r.Managers))
		}

		if members[nodes[4].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 4 to be unreachable")
		}

		if members[nodes[5].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 5 to be unreachable")
		}

		return nil
	}))

	// Restart the 2 nodes
	nodes[4] = raftutils.RestartNode(t, clockSource, nodes[4], securityConfig, false)
	nodes[5] = raftutils.RestartNode(t, clockSource, nodes[5], securityConfig, false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// All the nodes should be reachable again
	r, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	members = getMap(t, r.Managers)
	assert.Equal(t, 5, len(ts.Server.raft.GetMemberlist()))
	assert.Equal(t, 5, len(r.Managers))
	for i := 1; i <= 5; i++ {
		assert.Equal(t, api.MemberStatus_REACHABLE, members[nodes[uint64(i)].Config.ID].Status.State)
	}

	// Switch the raft node used by the server
	ts.Server.raft = nodes[2].Node

	// Stop node 1 (leader)
	nodes[1].Stop()
	nodes[1].Server.Stop()

	newCluster := map[uint64]*raftutils.TestNode{
		2: nodes[2],
		3: nodes[3],
		4: nodes[4],
		5: nodes[5],
	}

	// Wait for the re-election to occur
	raftutils.WaitForCluster(t, clockSource, newCluster)

	// Node 1 should not be the leader anymore
	assert.NoError(t, raftutils.PollFunc(func() error {
		r, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
		if err != nil {
			return err
		}

		members = getMap(t, r.Managers)

		if members[nodes[1].Config.ID].Status.Leader {
			return fmt.Errorf("expected node 1 not to be the leader")
		}

		if members[nodes[1].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 1 to be unreachable")
		}

		return nil
	}))

	// Restart node 1
	nodes[1].Shutdown()
	nodes[1] = raftutils.RestartNode(t, clockSource, nodes[1], securityConfig, false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Ensure that node 1 is not the leader
	assert.False(t, members[nodes[uint64(1)].Config.ID].Status.Leader)

	// Check that another node got the leader status
	leader := ""
	leaderCount := 0
	for i := 1; i <= 5; i++ {
		if members[nodes[uint64(i)].Config.ID].Status.Leader {
			leader = members[nodes[uint64(i)].Config.ID].ID
			leaderCount++
		}
	}

	// There should be only one leader after node 1 recovery and it
	// should be different than node 1
	assert.Equal(t, leaderCount, 1)
	assert.NotEqual(t, leader, members[nodes[1].Config.ID].ID)
}

func TestRemoveManager(t *testing.T) {
	ts := newTestServer(t)

	nodes, clockSource := raftutils.NewRaftCluster(t, securityConfig)
	defer raftutils.TeardownCluster(t, nodes)

	// Assign one of the raft node to the test server
	ts.Server.raft = nodes[1].Node

	// There should be 3 reachable managers listed
	lsr, err := ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, lsr)
	members := getMap(t, lsr.Managers)
	assert.Equal(t, 3, len(ts.Server.raft.GetMemberlist()))
	assert.Equal(t, 3, len(lsr.Managers))

	// Try to remove an incorrect ID
	rmr, err := ts.Client.RemoveManager(context.Background(), &api.RemoveManagerRequest{ManagerID: "ffffff"})
	assert.Error(t, err)
	assert.Nil(t, rmr)
	mlist := ts.Server.raft.GetMemberlist()
	assert.Equal(t, 3, len(mlist))
	for i := 1; i <= 3; i++ {
		assert.NotNil(t, mlist[nodes[uint64(i)].Config.ID])
	}
	assert.Equal(t, grpc.ErrorDesc(err), fmt.Sprintf(
		"member %s not found",
		"ffffff"),
	)

	// Stop node 2 and node 3 (2 nodes out of 3)
	nodes[2].Server.Stop()
	nodes[2].Shutdown()
	nodes[3].Server.Stop()
	nodes[3].Shutdown()

	// Node 2 and Node 3 should be listed as Unreachable
	assert.NoError(t, raftutils.PollFunc(func() error {
		lsr, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
		if err != nil {
			return err
		}

		members = getMap(t, lsr.Managers)

		if len(lsr.Managers) != 3 {
			return fmt.Errorf("expected 3 nodes, got %d", len(lsr.Managers))
		}

		if members[nodes[2].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 2 to be unreachable")
		}

		if members[nodes[3].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 3 to be unreachable")
		}

		return nil
	}))

	// We can't remove neither node 2 or node 3, this would potentially
	// result in a loss of quorum if one of the 2 nodes recover and
	// apply the configuration change on restart
	rmr, err = ts.Client.RemoveManager(context.Background(), &api.RemoveManagerRequest{ManagerID: members[nodes[2].Config.ID].ID})
	assert.Error(t, err)
	assert.Nil(t, rmr)
	mlist = ts.Server.raft.GetMemberlist()
	assert.Equal(t, 3, len(mlist))
	assert.NotNil(t, mlist[nodes[2].Config.ID])
	assert.Equal(t, grpc.ErrorDesc(err), fmt.Sprintf(
		"cannot remove member %s from the cluster: raft: member cannot be removed, because removing it may result in loss of quorum",
		mlist[nodes[2].Config.ID].ID),
	)

	rmr, err = ts.Client.RemoveManager(context.Background(), &api.RemoveManagerRequest{ManagerID: members[nodes[3].Config.ID].ID})
	assert.Error(t, err)
	assert.Nil(t, rmr)
	mlist = ts.Server.raft.GetMemberlist()
	assert.Equal(t, 3, len(mlist))
	assert.NotNil(t, mlist[nodes[3].Config.ID])
	assert.Equal(t, grpc.ErrorDesc(err), fmt.Sprintf(
		"cannot remove member %s from the cluster: raft: member cannot be removed, because removing it may result in loss of quorum",
		mlist[nodes[3].Config.ID].ID),
	)

	// Restart node 2 and node 3
	nodes[2] = raftutils.RestartNode(t, clockSource, nodes[2], securityConfig, false)
	nodes[3] = raftutils.RestartNode(t, clockSource, nodes[3], securityConfig, false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Stop node 3 again
	nodes[3].Server.Stop()

	// Node 3 should be listed as Unreachable
	assert.NoError(t, raftutils.PollFunc(func() error {
		lsr, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
		if err != nil {
			return err
		}

		members = getMap(t, lsr.Managers)

		if len(lsr.Managers) != 3 {
			return fmt.Errorf("expected 3 nodes, got %d", len(lsr.Managers))
		}

		if members[nodes[3].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 3 to be unreachable")
		}

		return nil
	}))

	// Try to remove node 3, this should succeed, we still have an active quorum
	rmr, err = ts.Client.RemoveManager(context.Background(), &api.RemoveManagerRequest{ManagerID: members[nodes[3].Config.ID].ID})
	assert.NoError(t, err)
	assert.NotNil(t, rmr)

	// Memberlist from both nodes should not list node 3
	assert.NoError(t, raftutils.PollFunc(func() error {
		mlist = ts.Server.raft.GetMemberlist()
		if len(mlist) != 2 {
			return fmt.Errorf("expected 2 nodes, got %d", len(mlist))
		}
		if mlist[nodes[3].Config.ID] != nil {
			return fmt.Errorf("expected memberlist not to list node 3")
		}

		ts.Server.raft = nodes[2].Node
		mlist = ts.Server.raft.GetMemberlist()
		if len(mlist) != 2 {
			return fmt.Errorf("expected 2 nodes, got %d", len(mlist))
		}
		if mlist[nodes[3].Config.ID] != nil {
			return fmt.Errorf("expected memberlist not to list node 3")
		}

		return nil
	}))

	// Switch back to node 1
	ts.Server.raft = nodes[1].Node

	// Loss of Quorum: stop node 2
	nodes[2].Server.Stop()

	// Node 2 should be listed as Unreachable
	assert.NoError(t, raftutils.PollFunc(func() error {
		lsr, err = ts.Client.ListManagers(context.Background(), &api.ListManagersRequest{})
		if err != nil {
			return err
		}

		members = getMap(t, lsr.Managers)

		if len(lsr.Managers) != 2 {
			return fmt.Errorf("expected 2 nodes, got %d", len(lsr.Managers))
		}

		if members[nodes[2].Config.ID].Status.State == api.MemberStatus_REACHABLE {
			return fmt.Errorf("expected node 2 to be unreachable")
		}

		return nil
	}))

	// Removing node 2 should fail, there is no quorum so the deadline on proposal should apply
	rmr, err = ts.Client.RemoveManager(context.Background(), &api.RemoveManagerRequest{ManagerID: members[nodes[2].Config.ID].ID})
	assert.Error(t, err)
	assert.Nil(t, rmr)
	mlist = ts.Server.raft.GetMemberlist()
	assert.Equal(t, 2, len(mlist))
	assert.NotNil(t, mlist[nodes[2].Config.ID])
	assert.Equal(t, grpc.ErrorDesc(err), fmt.Sprintf(
		"cannot remove member %s from the cluster: raft: member cannot be removed, because removing it may result in loss of quorum",
		mlist[nodes[2].Config.ID].ID),
	)
}
