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
}
