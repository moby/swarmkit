package manager

import (
	"io/ioutil"
	"math/rand"
	"net"
	"strconv"
	"testing"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/agent"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testManager struct {
	m      *Manager
	addr   string
	agents []*agent.Agent
}

func (tm *testManager) Close() {
	ctx, _ := context.WithTimeout(context.Background(), 3*time.Second)
	for _, a := range tm.agents {
		a.Stop(ctx)
	}
	tm.m.Stop()
}

type managersCluster struct {
	ms []*testManager
}

func (mc *managersCluster) Close() {
	for _, m := range mc.ms {
		m.Close()
	}
}

func newManager(t *testing.T, joinAddr string) *testManager {
	l, err := net.Listen("tcp", "127.0.0.1:0")

	stateDir, err := ioutil.TempDir("", "test-raft")
	require.NoError(t, err)

	m, err := New(&Config{
		Listener:         l,
		StateDir:         stateDir,
		DispatcherConfig: dispatcher.DefaultConfig(),
		JoinRaft:         joinAddr,
	})
	require.NoError(t, err)
	go m.Run()
	return &testManager{
		m:    m,
		addr: l.Addr().String(),
	}
}

func createManagersCluster(t *testing.T) *managersCluster {
	var managers []*testManager
	initManager := newManager(t, "")
	managers = append(managers, initManager)
	managers = append(managers, newManager(t, initManager.addr))
	managers = append(managers, newManager(t, initManager.addr))
	managers = append(managers, newManager(t, initManager.addr))
	managers = append(managers, newManager(t, initManager.addr))
	//for _, m := range managers {
	//addAgents(t, m)
	//}
	return &managersCluster{
		ms: managers,
	}
}

func addAgents(t *testing.T, m *testManager) {
	managers := agent.NewManagers(m.addr)
	for i := 0; i < 3; i++ {
		id := strconv.Itoa(rand.Int())
		a, err := agent.New(&agent.Config{
			ID:       id,
			Hostname: "hostname_" + id,
			Managers: managers,
			Executor: &NoopExecutor{},
		})
		require.NoError(t, err)
		require.NoError(t, a.Start(context.Background()))
		m.agents = append(m.agents, a)
	}
}

func TestCreateCluster(t *testing.T) {
	c := createManagersCluster(t)
	defer c.Close()
	for _, m := range c.ms {
		conn, err := grpc.Dial(m.addr, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
		assert.NoError(t, err)
		if err != nil {
			continue
		}
		mClient := api.NewManagerClient(conn)
		resp, err := mClient.NodeCount(context.Background(), nil)
		assert.NoError(t, err)
		if err != nil {
			continue
		}
		assert.Equal(t, 0, resp.Count)
	}
}
