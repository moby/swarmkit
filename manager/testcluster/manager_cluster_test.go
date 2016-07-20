package testcluster

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/swarmkit/agent"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	catestutils "github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager"
	"github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/picker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func init() {
	os.Setenv(ca.PassphraseENVVar, "")
	os.Setenv(ca.PassphraseENVVarPrev, "")
}

type testManager struct {
	m    *manager.Manager
	addr string
}

func (tm *testManager) Close() {
	tm.m.Stop(context.Background())
}

type managersCluster struct {
	ms     []*testManager
	agents []*agent.Agent
	tc     *catestutils.TestCA
}

func (mc *managersCluster) Close() {
	for _, m := range mc.ms {
		m.Close()
	}
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	for _, a := range mc.agents {
		a.Stop(ctx)
	}
	mc.tc.Stop()
}

func (mc *managersCluster) addAgents(count int) error {
	var addrs []api.Peer
	for _, m := range mc.ms {
		addrs = append(addrs, api.Peer{Addr: m.addr})
	}
	for i := 0; i < count; i++ {
		asConfig, err := mc.tc.NewNodeConfig(ca.AgentRole)
		if err != nil {
			return err
		}

		managers := picker.NewRemotes(addrs...)
		peer, err := managers.Select()
		if err != nil {
			return err
		}
		conn, err := grpc.Dial(peer.Addr,
			grpc.WithPicker(picker.NewPicker(managers)),
			grpc.WithTransportCredentials(asConfig.ClientTLSCreds))
		if err != nil {
			return err
		}

		id := strconv.Itoa(rand.Int())
		a, err := agent.New(&agent.Config{
			Hostname: "hostname_" + id,
			Managers: managers,
			Executor: &NoopExecutor{},
			Conn:     conn,
		})
		if err != nil {
			return err
		}
		if err := a.Start(context.Background()); err != nil {
			return err
		}
		mc.agents = append(mc.agents, a)
	}
	return nil
}

func (mc *managersCluster) addManagers(t *testing.T, count int) error {
	if len(mc.ms) == 0 {
		msConfig, err := mc.tc.NewNodeConfig(ca.ManagerRole)
		if err != nil {
			return err
		}
		initManager, err := newManager(t, "", msConfig)
		if err != nil {
			return err
		}
		mc.ms = append(mc.ms, initManager)
		count--
	}
	for i := 0; i < count; i++ {
		msConfig, err := mc.tc.NewNodeConfig(ca.ManagerRole)
		if err != nil {
			return err
		}
		mgr, err := newManager(t, mc.ms[0].addr, msConfig)
		if err != nil {
			return err
		}
		mc.ms = append(mc.ms, mgr)
	}
	return nil
}

func newManager(t *testing.T, joinAddr string, securityConfig *ca.SecurityConfig) (*testManager, error) {
	ltcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	stateDir, err := ioutil.TempDir("", "test-raft")
	if err != nil {
		return nil, err
	}

	m, err := manager.New(&manager.Config{
		ProtoListener:  map[string]net.Listener{"tcp": ltcp},
		StateDir:       stateDir,
		JoinRaft:       joinAddr,
		SecurityConfig: securityConfig,
	})
	if err != nil {
		return nil, err
	}
	go m.Run(context.Background())
	time.Sleep(100 * time.Millisecond)
	return &testManager{
		m:    m,
		addr: ltcp.Addr().String(),
	}, nil
}

func createManagersCluster(t *testing.T, managersCount, agentsCount int) *managersCluster {
	tc := catestutils.NewTestCA(t)
	defer tc.Stop()

	mc := &managersCluster{tc: tc}
	require.NoError(t, mc.addManagers(t, managersCount))
	time.Sleep(5 * time.Second)
	require.NoError(t, mc.addAgents(agentsCount))
	time.Sleep(10 * time.Second)
	return mc
}

var integrationTests = flag.Bool("integration", false, "run integration tests")

func (mc *managersCluster) pollRegister() error {
	var leaderFound bool
	var nodesFound int
	for _, m := range mc.ms {
		nCount := m.m.Dispatcher.NodeCount()
		if nCount != 0 {
			nodesFound = nCount
		}
		if nCount == len(mc.agents) {
			leaderFound = true
			break
		}
	}
	if !leaderFound {
		return fmt.Errorf("leader is not found - %d nodes registered, expected %d", nodesFound, len(mc.agents))
	}
	return nil
}

func (mc *managersCluster) destroyLeader() error {
	var leader *testManager
	var newMs []*testManager
	for _, m := range mc.ms {
		if m.m.RaftNode.IsLeader() {
			leader = m
			continue
		}
		newMs = append(newMs, m)
	}
	if leader == nil {
		return fmt.Errorf("leader is not found for destroy")
	}
	leader.m.Stop(context.Background())
	mc.ms = newMs
	return nil
}

func (mc *managersCluster) destroyAgents(count int) error {
	if count > len(mc.agents) {
		return fmt.Errorf("can't destroy %d agents, only %d is running", count, len(mc.agents))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	errCh := make(chan error, count)
	var wg sync.WaitGroup
	for _, a := range mc.agents[:count] {
		wg.Add(1)
		go func(a *agent.Agent) {
			errCh <- a.Stop(ctx)
			wg.Done()
		}(a)
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			return err
		}
	}
	mc.agents = mc.agents[count:]
	return nil
}

func (mc *managersCluster) leader() (*testManager, error) {
	for _, m := range mc.ms {
		if m.m.RaftNode.IsLeader() {
			return m, nil
		}
	}
	return nil, fmt.Errorf("leader is not found")
}

func TestCluster(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	c := createManagersCluster(t, 5, 15)
	defer c.Close()
	assert.NoError(t, testutils.PollFunc(nil, c.pollRegister))
	m := c.ms[0]
	nCount := m.m.Dispatcher.NodeCount()
	assert.Equal(t, 15, nCount)
}

func TestClusterReelection(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	mCount, aCount := 5, 15
	c := createManagersCluster(t, mCount, aCount)
	require.NoError(t, testutils.PollFunc(nil, c.pollRegister))

	require.NoError(t, c.destroyLeader())
	// let's down some managers in the meantime
	require.NoError(t, c.destroyAgents(5))
	// ensure that cluster will converge to expected number of agents, we need big timeout because of heartbeat times
	require.NoError(t, testutils.PollFuncWithTimeout(nil, c.pollRegister, 30*time.Second))

	leader, err := c.leader()
	assert.NoError(t, err)

	// check nodes in store
	var nodes []*api.Node
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		ns, err := store.FindNodes(tx, store.All)
		assert.NoError(t, err)
		for _, n := range ns {
			if n.Spec.Role == api.NodeRoleWorker {
				nodes = append(nodes, n)
			}
		}
	})
	assert.NoError(t, err)
	assert.Len(t, nodes, aCount, "there should be all nodes in store")
	var downAgentsCount int
	for _, node := range nodes {
		if node.Status.State == api.NodeStatus_DOWN {
			downAgentsCount++
			continue
		}
		assert.Equal(t, api.NodeStatus_READY, node.Status.State, "there should be only down and ready nodes at this point")
	}
	assert.Equal(t, 5, downAgentsCount, "unexpected number of down agents")
}

func TestClusterStoreAddPasshphraseForRootCA(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	defer os.Setenv(ca.PassphraseENVVar, "")
	defer os.Setenv(ca.PassphraseENVVarPrev, "")

	mCount, aCount := 5, 15
	c := createManagersCluster(t, mCount, aCount)
	require.NoError(t, testutils.PollFunc(nil, c.pollRegister))

	// Get the leader
	leader, err := c.leader()
	assert.NoError(t, err)

	// check key material in store
	var clusters []*api.Cluster
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err = store.FindClusters(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, clusters, 1, "there should be one cluster")
	assert.NotNil(t, clusters[0].RootCA.CACert)
	assert.NotNil(t, clusters[0].RootCA.CAKey)
	assert.NotContains(t, string(clusters[0].RootCA.CAKey), "ENCRYPTED")

	// Set an ENV passphrase and kill the current leader
	os.Setenv(ca.PassphraseENVVar, "password1")
	require.NoError(t, c.destroyLeader())

	// ensure that cluster will converge to expected number of agents, we need big timeout because of heartbeat times
	require.NoError(t, testutils.PollFuncWithTimeout(nil, c.pollRegister, 30*time.Second))

	// Get the new leader
	leader, err = c.leader()
	assert.NoError(t, err)
	// check key material in store
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err = store.FindClusters(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, clusters, 1, "there should be one cluster")
	assert.NotNil(t, clusters[0].RootCA.CACert)
	assert.NotNil(t, clusters[0].RootCA.CAKey)
	assert.Contains(t, string(clusters[0].RootCA.CAKey), "Proc-Type: 4,ENCRYPTED")

}

func TestClusterStoreWithPasshphraseForRootCA(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	// Start with a passphrase from moment 0
	os.Setenv(ca.PassphraseENVVar, "password1")
	defer os.Setenv(ca.PassphraseENVVar, "")
	defer os.Setenv(ca.PassphraseENVVarPrev, "")

	mCount, aCount := 5, 15
	c := createManagersCluster(t, mCount, aCount)
	require.NoError(t, testutils.PollFunc(nil, c.pollRegister))

	// Get the leader
	leader, err := c.leader()
	assert.NoError(t, err)

	// check key material in store
	var clusters []*api.Cluster
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err = store.FindClusters(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, clusters, 1, "there should be one cluster")
	assert.NotNil(t, clusters[0].RootCA.CACert)
	assert.NotNil(t, clusters[0].RootCA.CAKey)
	assert.Contains(t, string(clusters[0].RootCA.CAKey), "Proc-Type: 4,ENCRYPTED")
}

func TestClusterStorePasshphraseRotationForRootCA(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	os.Setenv(ca.PassphraseENVVar, "password1")
	defer os.Setenv(ca.PassphraseENVVar, "")
	defer os.Setenv(ca.PassphraseENVVarPrev, "")

	mCount, aCount := 5, 15
	c := createManagersCluster(t, mCount, aCount)
	require.NoError(t, testutils.PollFunc(nil, c.pollRegister))

	// Get the leader
	leader, err := c.leader()
	assert.NoError(t, err)

	// check key material in store
	var clusters []*api.Cluster
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err = store.FindClusters(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, clusters, 1, "there should be one cluster")
	assert.NotNil(t, clusters[0].RootCA.CACert)
	assert.NotNil(t, clusters[0].RootCA.CAKey)
	assert.Contains(t, string(clusters[0].RootCA.CAKey), "Proc-Type: 4,ENCRYPTED")

	firstEncryptedKey := clusters[0].RootCA.CAKey

	// Set an ENV passphrase and kill the current leader
	os.Setenv(ca.PassphraseENVVarPrev, "password1")
	os.Setenv(ca.PassphraseENVVar, "password2")
	require.NoError(t, c.destroyLeader())

	// ensure that cluster will converge to expected number of agents, we need big timeout because of heartbeat times
	require.NoError(t, testutils.PollFuncWithTimeout(nil, c.pollRegister, 30*time.Second))

	// Get the new leader
	leader, err = c.leader()
	assert.NoError(t, err)
	// check key material in store
	leader.m.RaftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err = store.FindClusters(tx, store.All)
	})
	assert.NoError(t, err)
	assert.Len(t, clusters, 1, "there should be one cluster")
	assert.NotNil(t, clusters[0].RootCA.CACert)
	assert.NotNil(t, clusters[0].RootCA.CAKey)
	assert.Contains(t, string(clusters[0].RootCA.CAKey), "Proc-Type: 4,ENCRYPTED")
	assert.NotEqual(t, firstEncryptedKey, clusters[0].RootCA.CAKey)

}
