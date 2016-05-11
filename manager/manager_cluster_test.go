package manager

import (
	"flag"
	"fmt"
	"io/ioutil"
	"math/rand"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/agent"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	catestutils "github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/state/raft/testutils"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testManager struct {
	m    *Manager
	addr string
}

func (tm *testManager) Close() {
	tm.m.Stop()
}

type managersCluster struct {
	ms     []*testManager
	agents []*agent.Agent
	tca    *catestutils.TestCA
}

func (mc *managersCluster) Close() {
	for _, m := range mc.ms {
		m.Close()
	}
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	for _, a := range mc.agents {
		a.Stop(ctx)
	}
	mc.tca.Close()
}

func (mc *managersCluster) AddAgents(count int) error {
	var addrs []string
	for _, m := range mc.ms {
		addrs = append(addrs, m.addr)
	}
	for i := 0; i < count; i++ {
		asConfig, err := mc.tca.AgentConfig()
		if err != nil {
			return err
		}
		managers := agent.NewManagers(addrs...)
		id := strconv.Itoa(rand.Int())
		a, err := agent.New(&agent.Config{
			Hostname:       "hostname_" + id,
			Managers:       managers,
			Executor:       &NoopExecutor{},
			SecurityConfig: asConfig,
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

func (mc *managersCluster) AddManagers(count int) error {
	if len(mc.ms) == 0 {
		msConfig, err := mc.tca.ManagerConfig()
		if err != nil {
			return err
		}
		initManager, err := newManager("", msConfig)
		if err != nil {
			return err
		}
		mc.ms = append(mc.ms, initManager)
		count--
	}
	for i := 0; i < count; i++ {
		msConfig, err := mc.tca.ManagerConfig()
		if err != nil {
			return err
		}
		mgr, err := newManager(mc.ms[0].addr, msConfig)
		if err != nil {
			return err
		}
		mc.ms = append(mc.ms, mgr)
	}
	return nil
}

func newManager(joinAddr string, securityConfig *ca.ManagerSecurityConfig) (*testManager, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	stateDir, err := ioutil.TempDir("", "test-raft")
	if err != nil {
		return nil, err
	}

	m, err := New(&Config{
		Listener:       l,
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
		addr: l.Addr().String(),
	}, nil
}

func createManagersCluster(t *testing.T, managersCount, agentsCount int) *managersCluster {
	tca, err := catestutils.NewTestCA()
	require.NoError(t, err)
	mc := &managersCluster{tca: tca}
	require.NoError(t, mc.AddManagers(managersCount))
	time.Sleep(5 * time.Second)
	require.NoError(t, mc.AddAgents(agentsCount))
	time.Sleep(10 * time.Second)
	return mc
}

var integrationTests = flag.Bool("integration", false, "run integration tests")

func (mc *managersCluster) pollRegister() error {
	var leaderFound bool
	var nodesFound int
	for _, m := range mc.ms {
		nCount := m.m.dispatcher.NodeCount()
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
		if m.m.raftNode.IsLeader() {
			leader = m
			continue
		}
		newMs = append(newMs, m)
	}
	if leader == nil {
		return fmt.Errorf("leader is not found for destroy")
	}
	leader.m.Stop()
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
		if m.m.raftNode.IsLeader() {
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
	assert.NoError(t, testutils.PollFunc(c.pollRegister))
	m := c.ms[0]
	nCount := m.m.dispatcher.NodeCount()
	assert.Equal(t, 15, nCount)
}

func TestClusterReelection(t *testing.T) {
	if !*integrationTests {
		t.Skip("integration test")
	}
	mCount, aCount := 5, 15
	c := createManagersCluster(t, mCount, aCount)
	require.NoError(t, testutils.PollFunc(c.pollRegister))

	require.NoError(t, c.destroyLeader())
	// let's down some managers in the meantime
	require.NoError(t, c.destroyAgents(5))
	// ensure that cluster will converge to expected number of agents, we need big timeout because of heartbeat times
	require.NoError(t, testutils.PollFuncWithTimeout(c.pollRegister, 30*time.Second))

	leader, err := c.leader()
	assert.NoError(t, err)

	// check nodes in store
	var nodes []*api.Node
	leader.m.raftNode.MemoryStore().View(func(tx store.ReadTx) {
		nodes, err = store.FindNodes(tx, store.All)
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
