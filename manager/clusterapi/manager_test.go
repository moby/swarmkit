package clusterapi

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strconv"
	"testing"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	cautils "github.com/docker/swarm-v2/ca/testutils"
	raftutils "github.com/docker/swarm-v2/manager/state/raft/testutils"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/grpclog"
)

var securityConfig *ca.ManagerSecurityConfig

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	logrus.SetOutput(ioutil.Discard)
	var tmpDir string
	_, securityConfig, tmpDir, _ = cautils.GenerateAgentAndManagerSecurityConfig(1)
	defer os.RemoveAll(tmpDir)
}

func getMap(t *testing.T, members []*api.Member) map[uint64]*api.Member {
	m := make(map[uint64]*api.Member)
	for _, member := range members {
		id, err := strconv.ParseUint(member.ID, 16, 64)
		assert.NoError(t, err)
		assert.NotZero(t, id)
		m[id] = member
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
	nodes[4] = raftutils.RestartNode(t, clockSource, nodes[4], securityConfig)
	nodes[5] = raftutils.RestartNode(t, clockSource, nodes[5], securityConfig)
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
	nodes[1] = raftutils.RestartNode(t, clockSource, nodes[1], securityConfig)
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
