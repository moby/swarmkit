package integration

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"testing"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarmkit/api"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

var showTrace = flag.Bool("show-trace", false, "show stack trace after tests finish")

func printTrace() {
	var (
		buf       []byte
		stackSize int
	)
	bufferLen := 16384
	for stackSize == len(buf) {
		buf = make([]byte, bufferLen)
		stackSize = runtime.Stack(buf, true)
		bufferLen *= 2
	}
	buf = buf[:stackSize]
	logrus.Error("===========================STACK TRACE===========================")
	fmt.Println(string(buf))
	logrus.Error("===========================STACK TRACE END=======================")
}

func TestMain(m *testing.M) {
	flag.Parse()
	res := m.Run()
	if *showTrace {
		printTrace()
	}
	os.Exit(res)
}

// pollClusterReady calls control api until all conditions are true:
// * all nodes are ready
// * all managers has membership == accepted
// * all managers has reachability == reachable
// * one node is leader
// * number of workers and managers equals to expected
func pollClusterReady(t *testing.T, c *testCluster, numWorker, numManager int) {
	pollFunc := func() error {
		res, err := c.api.ListNodes(context.Background(), &api.ListNodesRequest{})
		if err != nil {
			return err
		}
		var mCount int
		var leaderFound bool
		for _, n := range res.Nodes {
			if n.Status.State != api.NodeStatus_READY {
				return fmt.Errorf("node %s with desired role %s isn't ready, status %s, message %s", n.ID, n.Spec.DesiredRole, n.Status.State, n.Status.Message)
			}
			if n.Spec.Membership != api.NodeMembershipAccepted {
				return fmt.Errorf("node %s with desired role %s isn't accepted to cluster, membership %s", n.ID, n.Spec.DesiredRole, n.Spec.Membership)
			}
			if n.Certificate.Role != n.Spec.DesiredRole {
				return fmt.Errorf("node %s had different roles in spec and certificate, %s and %s respectively", n.ID, n.Spec.DesiredRole, n.Certificate.Role)
			}
			if n.Certificate.Status.State != api.IssuanceStateIssued {
				return fmt.Errorf("node %s with desired role %s has no issued certificate, issuance state %s", n.ID, n.Spec.DesiredRole, n.Certificate.Status.State)
			}
			if n.Role == api.NodeRoleManager {
				if n.ManagerStatus == nil {
					return fmt.Errorf("manager node %s has no ManagerStatus field", n.ID)
				}
				if n.ManagerStatus.Reachability != api.RaftMemberStatus_REACHABLE {
					return fmt.Errorf("manager node %s has reachable status: %s", n.ID, n.ManagerStatus.Reachability)
				}
				mCount++
				if n.ManagerStatus.Leader {
					leaderFound = true
				}
			} else {
				if n.ManagerStatus != nil {
					return fmt.Errorf("worker node %s should not have manager status, returned %s", n.ID, n.ManagerStatus)
				}
			}
		}
		if !leaderFound {
			return fmt.Errorf("leader of cluster is not found")
		}
		wCount := len(res.Nodes) - mCount
		if mCount != numManager {
			return fmt.Errorf("unexpected number of managers: %d, expected %d", mCount, numManager)
		}
		if wCount != numWorker {
			return fmt.Errorf("unexpected number of workers: %d, expected %d", wCount, numWorker)
		}
		return nil
	}
	err := raftutils.PollFuncWithTimeout(nil, pollFunc, opsTimeout)
	require.NoError(t, err)
}

func pollServiceReady(t *testing.T, c *testCluster, sid string) {
	pollFunc := func() error {
		req := &api.ListTasksRequest{}
		res, err := c.api.ListTasks(context.Background(), req)
		require.NoError(t, err)
		if len(res.Tasks) == 0 {
			return fmt.Errorf("tasks list is empty")
		}
		for _, task := range res.Tasks {
			if task.Status.State != api.TaskStateRunning {
				return fmt.Errorf("task %s is not running, status %s", task.ID, task.Status.State)
			}
		}
		return nil
	}
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, pollFunc, opsTimeout))
}

func newCluster(t *testing.T, numWorker, numManager int) *testCluster {
	cl := newTestCluster()
	for i := 0; i < numManager; i++ {
		require.NoError(t, cl.AddManager(), "manager number %d", i+1)
	}
	for i := 0; i < numWorker; i++ {
		require.NoError(t, cl.AddAgent(), "agent number %d", i+1)
	}

	pollClusterReady(t, cl, numWorker, numManager)
	return cl
}

func TestClusterCreate(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 0, 2
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()
}

func TestServiceCreate(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 3, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	sid, err := cl.CreateService("test_service", 60)
	require.NoError(t, err)
	pollServiceReady(t, cl, sid)
}

func TestNodeOps(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	// demote leader
	leader, err := cl.Leader()
	require.NoError(t, err)
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleWorker))
	// agents 2, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)

	// remove node
	var worker *testNode
	for _, n := range cl.nodes {
		if !n.IsManager() && n.node.NodeID() != leader.node.NodeID() {
			worker = n
			break
		}
	}
	require.NoError(t, cl.RemoveNode(worker.node.NodeID(), false))
	// agents 1, managers 2
	numWorker--
	// long wait for heartbeat expiration
	pollClusterReady(t, cl, numWorker, numManager)

	// promote old leader back
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleManager))
	numWorker--
	numManager++
	// agents 0, managers 3
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestDemotePromote(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	leader, err := cl.Leader()
	require.NoError(t, err)
	var manager *testNode
	for _, n := range cl.nodes {
		if n.IsManager() && n.node.NodeID() != leader.node.NodeID() {
			manager = n
			break
		}
	}
	require.NoError(t, cl.SetNodeRole(manager.node.NodeID(), api.NodeRoleWorker))
	// agents 2, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)

	// promote same node
	require.NoError(t, cl.SetNodeRole(manager.node.NodeID(), api.NodeRoleManager))
	// agents 1, managers 3
	numWorker--
	numManager++
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestPromoteDemote(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	var worker *testNode
	for _, n := range cl.nodes {
		if !n.IsManager() {
			worker = n
			break
		}
	}
	require.NoError(t, cl.SetNodeRole(worker.node.NodeID(), api.NodeRoleManager))
	// agents 0, managers 4
	numWorker--
	numManager++
	pollClusterReady(t, cl, numWorker, numManager)

	// demote same node
	require.NoError(t, cl.SetNodeRole(worker.node.NodeID(), api.NodeRoleWorker))
	// agents 1, managers 3
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestDemotePromoteLeader(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	leader, err := cl.Leader()
	require.NoError(t, err)
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleWorker))
	// agents 2, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)

	//promote former leader back
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleManager))
	// agents 1, managers 3
	numWorker--
	numManager++
	pollClusterReady(t, cl, numWorker, numManager)
}

// TODO: improve test to demote the leader in case of 2 remaining managers
func TestDemoteToSingleManager(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	leader, err := cl.Leader()
	require.NoError(t, err)
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleWorker))
	// agents 2, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)

	leader, err = cl.Leader()
	require.NoError(t, err)
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleWorker))
	// agents 3, managers 1
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestDemoteLeader(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 1, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	leader, err := cl.Leader()
	require.NoError(t, err)
	require.NoError(t, cl.SetNodeRole(leader.node.NodeID(), api.NodeRoleWorker))
	// agents 2, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestDemoteDownedManager(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 0, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()

	leader, err := cl.Leader()
	require.NoError(t, err)

	// add a new manager so we have 3, then find one (not the leader) to demote
	var demotee *testNode
	for _, n := range cl.nodes {
		if n.IsManager() && n.node.NodeID() != leader.node.NodeID() {
			demotee = n
			break
		}
	}

	nodeID := demotee.node.NodeID()

	resp, err := cl.api.GetNode(context.Background(), &api.GetNodeRequest{NodeID: nodeID})
	require.NoError(t, err)
	spec := resp.Node.Spec.Copy()
	spec.DesiredRole = api.NodeRoleWorker

	// stop the node, then demote it, and start it back up again so when it comes back up it has to realize
	// it's not running anymore
	require.NoError(t, demotee.Pause())

	// demote node, but don't use SetNodeRole, which waits until it successfully becomes a worker, since
	// the node is currently down
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		_, err := cl.api.UpdateNode(context.Background(), &api.UpdateNodeRequest{
			NodeID:      nodeID,
			Spec:        spec,
			NodeVersion: &resp.Node.Meta.Version,
		})
		return err
	}, opsTimeout))

	// start it back up again
	require.NoError(t, cl.StartNode(nodeID))

	// wait to become worker
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		if demotee.IsManager() {
			return fmt.Errorf("node is still not a worker")
		}
		return nil
	}, opsTimeout))

	// agents 1, managers 2
	numWorker++
	numManager--
	pollClusterReady(t, cl, numWorker, numManager)
}

func TestRestartLeader(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 5, 3
	cl := newCluster(t, numWorker, numManager)
	defer func() {
		require.NoError(t, cl.Stop())
	}()
	leader, err := cl.Leader()
	require.NoError(t, err)

	origLeaderID := leader.node.NodeID()

	require.NoError(t, leader.Pause())

	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		resp, err := cl.api.ListNodes(context.Background(), &api.ListNodesRequest{})
		if err != nil {
			return err
		}
		for _, node := range resp.Nodes {
			if node.ID == origLeaderID {
				continue
			}
			require.False(t, node.Status.State == api.NodeStatus_DOWN, "nodes shouldn't go to down")
			if node.Status.State != api.NodeStatus_READY {
				return errors.Errorf("node %s is still not ready", node.ID)
			}
		}
		return nil
	}, opsTimeout))

	require.NoError(t, cl.StartNode(origLeaderID))

	pollClusterReady(t, cl, numWorker, numManager)
}
