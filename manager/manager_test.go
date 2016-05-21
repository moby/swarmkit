package manager

import (
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

// NoopExecutor is a dummy executor that implements enough to get the agent started.
type NoopExecutor struct {
}

func (e *NoopExecutor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	return &api.NodeDescription{}, nil
}

func (e *NoopExecutor) Controller(t *api.Task) (exec.Controller, error) {
	return nil, exec.ErrRuntimeUnsupported
}

func TestManager(t *testing.T) {
	ctx := context.TODO()
	store := store.NewMemoryStore(nil)
	assert.NotNil(t, store)

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err)
	defer os.RemoveAll(stateDir)

	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	agentSecurityConfig, err := tc.NewNodeConfig(ca.AgentRole)
	assert.NoError(t, err)
	managerSecurityConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	m, err := New(&Config{
		ListenProto:    "unix",
		ListenAddr:     temp.Name(),
		StateDir:       stateDir,
		SecurityConfig: managerSecurityConfig,
	})
	assert.NoError(t, err)
	assert.NotNil(t, m)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- m.Run(ctx)
	}()

	opts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	opts = append(opts, grpc.WithTransportCredentials(agentSecurityConfig.ClientTLSCreds))
	opts = append(opts, grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
		return net.DialTimeout("unix", addr, timeout)
	}))

	conn, err := grpc.Dial(temp.Name(), opts...)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	// We have to send a dummy request to verify if the connection is actually up.
	client := api.NewDispatcherClient(conn)
	_, err = client.Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Equal(t, grpc.ErrorDesc(err), dispatcher.ErrNodeNotRegistered.Error())

	m.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}
