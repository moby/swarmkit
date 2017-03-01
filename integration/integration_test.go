package integration

import (
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	"github.com/cloudflare/cfssl/helpers"
	events "github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
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
	ca.RenewTLSExponentialBackoff = events.ExponentialBackoffConfig{
		Factor: time.Millisecond * 500,
		Max:    time.Minute,
	}
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

func pollServiceReady(t *testing.T, c *testCluster, sid string, replicas int) {
	pollFunc := func() error {
		req := &api.ListTasksRequest{Filters: &api.ListTasksRequest_Filters{
			ServiceIDs: []string{sid},
		}}
		res, err := c.api.ListTasks(context.Background(), req)
		require.NoError(t, err)

		if len(res.Tasks) == 0 {
			return fmt.Errorf("tasks list is empty")
		}
		var running int
		var states []string
		for _, task := range res.Tasks {
			if task.Status.State == api.TaskStateRunning {
				running++
			}
			states = append(states, fmt.Sprintf("[task %s: %s]", task.ID, task.Status.State))
		}
		if running != replicas {
			return fmt.Errorf("only %d running tasks, but expecting %d replicas: %s", running, replicas, strings.Join(states, ", "))
		}

		return nil
	}
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, pollFunc, opsTimeout))
}

func newCluster(t *testing.T, numWorker, numManager int) *testCluster {
	cl := newTestCluster()
	for i := 0; i < numManager; i++ {
		require.NoError(t, cl.AddManager(false, nil), "manager number %d", i+1)
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

func TestServiceCreateLateBind(t *testing.T) {
	t.Parallel()

	numWorker, numManager := 3, 3

	cl := newTestCluster()
	for i := 0; i < numManager; i++ {
		require.NoError(t, cl.AddManager(true, nil), "manager number %d", i+1)
	}
	for i := 0; i < numWorker; i++ {
		require.NoError(t, cl.AddAgent(), "agent number %d", i+1)
	}

	defer func() {
		require.NoError(t, cl.Stop())
	}()

	sid, err := cl.CreateService("test_service", 60)
	require.NoError(t, err)
	pollServiceReady(t, cl, sid, 60)
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
	pollServiceReady(t, cl, sid, 60)
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

	// Find a manager (not the leader) to demote. It must not be the third
	// manager we added, because there may not have been enough time for
	// that one to write anything to its WAL.
	var demotee *testNode
	for _, n := range cl.nodes {
		nodeID := n.node.NodeID()
		if n.IsManager() && nodeID != leader.node.NodeID() && cl.nodesOrder[nodeID] != 3 {
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
	require.NoError(t, demotee.Pause(false))

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

	require.NoError(t, leader.Pause(false))

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

func TestForceNewCluster(t *testing.T) {
	t.Parallel()

	// create an external CA so that we can use it to generate expired certificates
	tempDir, err := ioutil.TempDir("", "external-ca")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	rootCA, err := ca.CreateRootCA("externalRoot", ca.NewConfigPaths(tempDir).RootCA)
	require.NoError(t, err)

	// start a new cluster with the external CA bootstrapped
	numWorker, numManager := 0, 1
	cl := newTestCluster()
	defer func() {
		require.NoError(t, cl.Stop())
	}()
	require.NoError(t, cl.AddManager(false, &rootCA), "manager number 1")
	pollClusterReady(t, cl, numWorker, numManager)

	leader, err := cl.Leader()
	require.NoError(t, err)

	sid, err := cl.CreateService("test_service", 2)
	require.NoError(t, err)
	pollServiceReady(t, cl, sid, 2)

	// generate an expired certificate
	managerCertFile := filepath.Join(leader.stateDir, "certificates", "swarm-node.crt")
	certBytes, err := ioutil.ReadFile(managerCertFile)
	require.NoError(t, err)
	now := time.Now()
	// we don't want it too expired, because it can't have expired before the root CA cert is valid
	expiredCertPEM := testutils.ReDateCert(t, certBytes, rootCA.Cert, rootCA.Signer.Key, now.Add(-1*time.Hour), now.Add(-1*time.Second))

	// restart node with an expired certificate while forcing a new cluster - it should start without error and the certificate should be renewed
	nodeID := leader.node.NodeID()
	require.NoError(t, leader.Pause(true))
	require.NoError(t, ioutil.WriteFile(managerCertFile, expiredCertPEM, 0644))
	require.NoError(t, cl.StartNode(nodeID))
	pollClusterReady(t, cl, numWorker, numManager)
	pollServiceReady(t, cl, sid, 2)

	err = raftutils.PollFuncWithTimeout(nil, func() error {
		certBytes, err := ioutil.ReadFile(managerCertFile)
		if err != nil {
			return err
		}
		managerCerts, err := helpers.ParseCertificatesPEM(certBytes)
		if err != nil {
			return err
		}
		if managerCerts[0].NotAfter.Before(time.Now()) {
			return errors.New("certificate hasn't been renewed yet")
		}
		return nil
	}, opsTimeout)
	require.NoError(t, err)

	// restart node with an expired certificate without forcing a new cluster - it should error on start
	require.NoError(t, leader.Pause(true))
	require.NoError(t, ioutil.WriteFile(managerCertFile, expiredCertPEM, 0644))
	require.Error(t, cl.StartNode(nodeID))
}
