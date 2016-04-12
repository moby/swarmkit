package manager

import (
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/docker/swarm-v2/agent"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/state"
	dispatcherpb "github.com/docker/swarm-v2/pb/docker/cluster/api/dispatcher"
	managerpb "github.com/docker/swarm-v2/pb/docker/cluster/api/manager"
	objectspb "github.com/docker/swarm-v2/pb/docker/cluster/objects"
	typespb "github.com/docker/swarm-v2/pb/docker/cluster/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

// NoopExecutor is a dummy executor that implements enough to get the agent started.
type NoopExecutor struct {
}

func (e *NoopExecutor) Describe(ctx context.Context) (*typespb.NodeDescription, error) {
	return &typespb.NodeDescription{}, nil
}

func (e *NoopExecutor) Runner(t *objectspb.Task) (exec.Runner, error) {
	return nil, exec.ErrRuntimeUnsupported
}

func TestManager(t *testing.T) {
	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err)
	defer os.RemoveAll(stateDir)

	m, err := New(&Config{
		ListenProto: "unix",
		ListenAddr:  temp.Name(),
		StateDir:    stateDir,
	})
	assert.NoError(t, err)
	assert.NotNil(t, m)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- m.Run()
	}()

	conn, err := grpc.Dial(temp.Name(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	// We have to send a dummy request to verify if the connection is actually up.
	client := dispatcherpb.NewDispatcherClient(conn)
	_, err = client.Heartbeat(context.Background(), &dispatcherpb.HeartbeatRequest{NodeID: "foo"})
	assert.Equal(t, grpc.ErrorDesc(err), dispatcher.ErrNodeNotRegistered.Error())

	m.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestManagerNodeCount(t *testing.T) {
	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	l, err := net.Listen("tcp", "127.0.0.1:0")

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err)
	defer os.RemoveAll(stateDir)

	m, err := New(&Config{
		Listener: l,
		StateDir: stateDir,
	})
	assert.NoError(t, err)
	assert.NotNil(t, m)
	go m.Run()
	defer m.Stop()

	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout(l.Addr().Network(), addr, timeout)
		}))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	// We have to send a dummy request to verify if the connection is actually up.
	mClient := managerpb.NewManagerClient(conn)

	managers := agent.NewManagers(l.Addr().String())
	a1, err := agent.New(&agent.Config{
		ID:       "test1",
		Hostname: "hostname1",
		Managers: managers,
		Executor: &NoopExecutor{},
	})
	require.NoError(t, err)
	a2, err := agent.New(&agent.Config{
		ID:       "test2",
		Hostname: "hostname2",
		Managers: managers,
		Executor: &NoopExecutor{},
	})
	require.NoError(t, err)

	require.NoError(t, a1.Start(context.Background()))
	require.NoError(t, a2.Start(context.Background()))

	defer func() {
		ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
		a1.Stop(ctx)
		a2.Stop(ctx)
	}()

	time.Sleep(1500 * time.Millisecond)

	resp, err := mClient.NodeCount(context.Background(), &managerpb.NodeCountRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 2, resp.Count)
}
