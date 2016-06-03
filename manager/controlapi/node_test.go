package controlapi

import (
	"fmt"
	"io/ioutil"
	"log"
	"testing"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	cautils "github.com/docker/swarm-v2/ca/testutils"
	raftutils "github.com/docker/swarm-v2/manager/state/raft/testutils"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/grpclog"
)

func createNode(t *testing.T, ts *testServer, id string, role api.NodeSpec_Role, membership api.NodeSpec_Membership) *api.Node {
	node := &api.Node{
		ID: id,
		Spec: api.NodeSpec{
			Role:       role,
			Membership: membership,
		},
	}
	err := ts.Store.Update(func(tx store.Tx) error {
		return store.CreateNode(tx, node)
	})
	assert.NoError(t, err)
	return node
}

func TestGetNode(t *testing.T) {
	ts := newTestServer(t)

	_, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "invalid"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	node := createNode(t, ts, "id", api.NodeRoleManager, api.NodeMembershipAccepted)
	r, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: node.ID})
	assert.NoError(t, err)
	assert.Equal(t, node.ID, r.Node.ID)
}

func TestListNodes(t *testing.T) {
	ts := newTestServer(t)
	r, err := ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Empty(t, r.Nodes)

	createNode(t, ts, "id1", api.NodeRoleManager, api.NodeMembershipAccepted)
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Nodes))

	createNode(t, ts, "id2", api.NodeRoleWorker, api.NodeMembershipAccepted)
	createNode(t, ts, "id3", api.NodeRoleWorker, api.NodeMembershipRejected)
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Nodes))

	// List by role.
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Roles: []api.NodeSpec_Role{api.NodeRoleManager},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Nodes))
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Roles: []api.NodeSpec_Role{api.NodeRoleWorker},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Nodes))
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Roles: []api.NodeSpec_Role{api.NodeRoleManager, api.NodeRoleWorker},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Nodes))

	// List by membership.
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Memberships: []api.NodeSpec_Membership{api.NodeMembershipAccepted},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(r.Nodes))
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Memberships: []api.NodeSpec_Membership{api.NodeMembershipRejected},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Nodes))
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Memberships: []api.NodeSpec_Membership{api.NodeMembershipAccepted, api.NodeMembershipRejected},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(r.Nodes))
	r, err = ts.Client.ListNodes(context.Background(),
		&api.ListNodesRequest{
			Filters: &api.ListNodesRequest_Filters{
				Roles:       []api.NodeSpec_Role{api.NodeRoleWorker},
				Memberships: []api.NodeSpec_Membership{api.NodeMembershipRejected},
			},
		},
	)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(r.Nodes))
}

func init() {
	grpclog.SetLogger(log.New(ioutil.Discard, "", log.LstdFlags))
	logrus.SetOutput(ioutil.Discard)
}

func getMap(t *testing.T, nodes []*api.Node) map[uint64]*api.Manager {
	m := make(map[uint64]*api.Manager)
	for _, n := range nodes {
		if n.Manager != nil {
			m[n.Manager.Raft.RaftID] = n.Manager
		}
	}
	return m
}

func TestListManagerNodes(t *testing.T) {
	tc := cautils.NewTestCA(nil, cautils.AutoAcceptPolicy())
	ts := newTestServer(t)

	nodes, clockSource := raftutils.NewRaftCluster(t, tc)
	defer raftutils.TeardownCluster(t, nodes)

	// Create a node object for each of the managers
	assert.NoError(t, nodes[1].MemoryStore().Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNode(tx, &api.Node{ID: nodes[1].SecurityConfig.ClientTLSCreds.NodeID()}))
		assert.NoError(t, store.CreateNode(tx, &api.Node{ID: nodes[2].SecurityConfig.ClientTLSCreds.NodeID()}))
		assert.NoError(t, store.CreateNode(tx, &api.Node{ID: nodes[3].SecurityConfig.ClientTLSCreds.NodeID()}))
		return nil
	}))

	// Assign one of the raft node to the test server
	ts.Server.raft = nodes[1].Node
	ts.Server.store = nodes[1].MemoryStore()

	// There should be 3 reachable managers listed
	r, err := ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	managers := getMap(t, r.Nodes)
	assert.Len(t, ts.Server.raft.GetMemberlist(), 3)
	assert.Len(t, r.Nodes, 3)

	// Node 1 should be the leader
	for i := 1; i <= 3; i++ {
		if i == 1 {
			assert.True(t, managers[nodes[uint64(i)].Config.ID].Raft.Status.Leader)
			continue
		}
		assert.False(t, managers[nodes[uint64(i)].Config.ID].Raft.Status.Leader)
	}

	// All nodes should be reachable
	for i := 1; i <= 3; i++ {
		assert.Equal(t, api.RaftMemberStatus_REACHABLE, managers[nodes[uint64(i)].Config.ID].Raft.Status.Reachability)
	}

	// Add two more nodes to the cluster
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.AddRaftNode(t, clockSource, nodes, tc)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Add node entries for these
	assert.NoError(t, nodes[1].MemoryStore().Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNode(tx, &api.Node{ID: nodes[4].SecurityConfig.ClientTLSCreds.NodeID()}))
		assert.NoError(t, store.CreateNode(tx, &api.Node{ID: nodes[5].SecurityConfig.ClientTLSCreds.NodeID()}))
		return nil
	}))

	// There should be 5 reachable managers listed
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	managers = getMap(t, r.Nodes)
	assert.Len(t, ts.Server.raft.GetMemberlist(), 5)
	assert.Len(t, r.Nodes, 5)
	for i := 1; i <= 5; i++ {
		assert.Equal(t, api.RaftMemberStatus_REACHABLE, managers[nodes[uint64(i)].Config.ID].Raft.Status.Reachability)
	}

	// Stops 2 nodes
	nodes[4].Server.Stop()
	nodes[4].Shutdown()
	nodes[5].Server.Stop()
	nodes[5].Shutdown()

	// Node 4 and Node 5 should be listed as Unreachable
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
		if err != nil {
			return err
		}

		managers = getMap(t, r.Nodes)

		if len(r.Nodes) != 5 {
			return fmt.Errorf("expected 5 nodes, got %d", len(r.Nodes))
		}

		if managers[nodes[4].Config.ID].Raft.Status.Reachability == api.RaftMemberStatus_REACHABLE {
			return fmt.Errorf("expected node 4 to be unreachable")
		}

		if managers[nodes[5].Config.ID].Raft.Status.Reachability == api.RaftMemberStatus_REACHABLE {
			return fmt.Errorf("expected node 5 to be unreachable")
		}

		return nil
	}))

	// Restart the 2 nodes
	nodes[4] = raftutils.RestartNode(t, clockSource, nodes[4], false)
	nodes[5] = raftutils.RestartNode(t, clockSource, nodes[5], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// All the nodes should be reachable again
	r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.NoError(t, err)
	assert.NotNil(t, r)
	managers = getMap(t, r.Nodes)
	assert.Len(t, ts.Server.raft.GetMemberlist(), 5)
	assert.Len(t, r.Nodes, 5)
	for i := 1; i <= 5; i++ {
		assert.Equal(t, api.RaftMemberStatus_REACHABLE, managers[nodes[uint64(i)].Config.ID].Raft.Status.Reachability)
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
	assert.NoError(t, raftutils.PollFunc(clockSource, func() error {
		r, err = ts.Client.ListNodes(context.Background(), &api.ListNodesRequest{})
		if err != nil {
			return err
		}

		managers = getMap(t, r.Nodes)

		if managers[nodes[1].Config.ID].Raft.Status.Leader {
			return fmt.Errorf("expected node 1 not to be the leader")
		}

		if managers[nodes[1].Config.ID].Raft.Status.Reachability == api.RaftMemberStatus_REACHABLE {
			return fmt.Errorf("expected node 1 to be unreachable")
		}

		return nil
	}))

	// Restart node 1
	nodes[1].Shutdown()
	nodes[1] = raftutils.RestartNode(t, clockSource, nodes[1], false)
	raftutils.WaitForCluster(t, clockSource, nodes)

	// Ensure that node 1 is not the leader
	assert.False(t, managers[nodes[uint64(1)].Config.ID].Raft.Status.Leader)

	// Check that another node got the leader status
	var leader uint64
	leaderCount := 0
	for i := 1; i <= 5; i++ {
		if managers[nodes[uint64(i)].Config.ID].Raft.Status.Leader {
			leader = nodes[uint64(i)].Config.ID
			leaderCount++
		}
	}

	// There should be only one leader after node 1 recovery and it
	// should be different than node 1
	assert.Equal(t, 1, leaderCount)
	assert.NotEqual(t, leader, nodes[1].Config.ID)
}

func TestUpdateNode(t *testing.T) {
	ts := newTestServer(t)

	_, err := ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "invalid", Spec: &api.NodeSpec{}, NodeVersion: &api.Version{}})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
		NodeVersion: &api.Version{},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	createNode(t, ts, "id", api.NodeRoleManager, api.NodeMembershipAccepted)
	r, err := ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "id"})
	assert.NoError(t, err)
	assert.NotNil(t, r.Node)

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id"})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
	})
	assert.Error(t, err)
	assert.Equal(t, codes.InvalidArgument, grpc.Code(err))

	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{
		NodeID: "id",
		Spec: &api.NodeSpec{
			Availability: api.NodeAvailabilityDrain,
		},
		NodeVersion: &r.Node.Meta.Version,
	})
	assert.NoError(t, err)

	r, err = ts.Client.GetNode(context.Background(), &api.GetNodeRequest{NodeID: "id"})
	assert.NoError(t, err)
	assert.NotNil(t, r.Node)
	assert.NotNil(t, r.Node.Spec)
	assert.Equal(t, api.NodeAvailabilityDrain, r.Node.Spec.Availability)

	version := &r.Node.Meta.Version
	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id", Spec: &r.Node.Spec, NodeVersion: version})
	assert.NoError(t, err)
	// Perform an update with the "old" version.
	_, err = ts.Client.UpdateNode(context.Background(), &api.UpdateNodeRequest{NodeID: "id", Spec: &r.Node.Spec, NodeVersion: version})
	assert.Error(t, err)
}
