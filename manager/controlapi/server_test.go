package controlapi

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"google.golang.org/grpc"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	cautils "github.com/moby/swarmkit/v2/ca/testutils"
	"github.com/moby/swarmkit/v2/manager/state/store"
	stateutils "github.com/moby/swarmkit/v2/manager/state/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testServer struct {
	Server *Server
	Client api.ControlClient
	Store  *store.MemoryStore

	grpcServer *grpc.Server
	clientConn *grpc.ClientConn

	tempUnixSocket string
}

func (ts *testServer) Stop() {
	ts.clientConn.Close()
	ts.grpcServer.Stop()
	ts.Store.Close()
	os.RemoveAll(ts.tempUnixSocket)
}

func newTestServer(t *testing.T) *testServer {
	ts := &testServer{}

	// Create a testCA just to get a usable RootCA object
	tc := cautils.NewTestCA(t)
	securityConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	tc.Stop()
	require.NoError(t, err)

	ts.Store = store.NewMemoryStore(&stateutils.MockProposer{})
	assert.NotNil(t, ts.Store)

	ts.Server = NewServer(ts.Store, nil, securityConfig, nil, nil)
	assert.NotNil(t, ts.Server)

	temp, err := os.CreateTemp("", "test-socket")
	require.NoError(t, err)
	require.NoError(t, temp.Close())
	require.NoError(t, os.Remove(temp.Name()))

	ts.tempUnixSocket = temp.Name()

	lis, err := net.Listen("unix", temp.Name())
	require.NoError(t, err)

	ts.grpcServer = grpc.NewServer()
	api.RegisterControlServer(ts.grpcServer, ts.Server)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explicitly ignore it.
		_ = ts.grpcServer.Serve(lis)
	}()

	conn, err := grpc.Dial(temp.Name(), grpc.WithInsecure(), grpc.WithTimeout(10*time.Second),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("unix", addr, timeout)
		}))
	require.NoError(t, err)
	ts.clientConn = conn

	ts.Client = api.NewControlClient(conn)

	// Create ingress network
	ts.Client.CreateNetwork(context.Background(),
		&api.CreateNetworkRequest{
			Spec: &api.NetworkSpec{
				Ingress: true,
				Annotations: api.Annotations{
					Name: "test-ingress",
				},
			},
		})

	return ts
}
