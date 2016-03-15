package manager

import (
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/dispatcher"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/stretchr/testify/assert"
)

func TestManager(t *testing.T) {
	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	m := New(&Config{
		Store:       store,
		ListenProto: "unix",
		ListenAddr:  temp.Name(),
	})
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
	client := api.NewDispatcherClient(conn)
	_, err = client.Heartbeat(context.Background(), &api.HeartbeatRequest{NodeID: "foo"})
	assert.Equal(t, grpc.ErrorDesc(err), dispatcher.ErrNodeNotRegistered.Error())

	m.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestManagerNodeCount(t *testing.T) {
	store := state.NewMemoryStore(nil)
	assert.NotNil(t, store)

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	m := New(&Config{
		Store:       store,
		ListenProto: "unix",
		ListenAddr:  temp.Name(),
	})
	assert.NotNil(t, m)
	go m.Run()
	defer m.Stop()

	conn, err := grpc.Dial(temp.Name(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	// We have to send a dummy request to verify if the connection is actually up.
	mClient := api.NewManagerClient(conn)
	dClient := api.NewDispatcherClient(conn)

	_, err = dClient.Register(context.Background(), &api.RegisterRequest{NodeID: "test1"})
	assert.NoError(t, err)

	_, err = dClient.Register(context.Background(), &api.RegisterRequest{NodeID: "test2"})
	assert.NoError(t, err)

	resp, err := mClient.NodeCount(context.Background(), &api.NodeCountRequest{})
	assert.NoError(t, err)
	assert.Equal(t, 2, resp.Count)
}
