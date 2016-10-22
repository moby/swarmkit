package manager

import (
	"crypto/tls"
	"io/ioutil"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/dispatcher"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestManager(t *testing.T) {
	ctx := context.Background()

	temp, err := ioutil.TempFile("", "test-socket")
	assert.NoError(t, err)
	assert.NoError(t, temp.Close())
	assert.NoError(t, os.Remove(temp.Name()))

	defer os.RemoveAll(temp.Name())

	stateDir, err := ioutil.TempDir("", "test-raft")
	assert.NoError(t, err)
	defer os.RemoveAll(stateDir)

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	agentSecurityConfig, err := tc.NewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)
	agentDiffOrgSecurityConfig, err := tc.NewNodeConfigOrg(ca.WorkerRole, "another-org")
	assert.NoError(t, err)
	managerSecurityConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	m, err := New(&Config{
		RemoteAPI:        RemoteAddrs{ListenAddr: "127.0.0.1:0"},
		ControlAPI:       temp.Name(),
		StateDir:         stateDir,
		SecurityConfig:   managerSecurityConfig,
		UnlockKey:        []byte("kek"),
		AutoLockManagers: true,
	})
	assert.NoError(t, err)
	assert.NotNil(t, m)

	tcpAddr := m.Addr()

	done := make(chan error)
	defer close(done)
	go func() {
		done <- m.Run(ctx)
	}()

	opts := []grpc.DialOption{
		grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(agentSecurityConfig.ClientTLSCreds),
	}

	conn, err := grpc.Dial(tcpAddr, opts...)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn.Close())
	}()

	// We have to send a dummy request to verify if the connection is actually up.
	client := api.NewDispatcherClient(conn)
	_, err = client.Heartbeat(ctx, &api.HeartbeatRequest{})
	assert.Equal(t, dispatcher.ErrNodeNotRegistered.Error(), grpc.ErrorDesc(err))
	_, err = client.Session(ctx, &api.SessionRequest{})
	assert.NoError(t, err)

	// Try to have a client in a different org access this manager
	opts = []grpc.DialOption{
		grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(agentDiffOrgSecurityConfig.ClientTLSCreds),
	}

	conn2, err := grpc.Dial(tcpAddr, opts...)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, conn2.Close())
	}()

	client = api.NewDispatcherClient(conn2)
	_, err = client.Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Contains(t, grpc.ErrorDesc(err), "Permission denied: unauthorized peer role: rpc error: code = 7 desc = Permission denied: remote certificate not part of organization")

	// Verify that requests to the various GRPC services running on TCP
	// are rejected if they don't have certs.
	opts = []grpc.DialOption{
		grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})),
	}

	noCertConn, err := grpc.Dial(tcpAddr, opts...)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, noCertConn.Close())
	}()

	client = api.NewDispatcherClient(noCertConn)
	_, err = client.Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")

	controlClient := api.NewControlClient(noCertConn)
	_, err = controlClient.ListNodes(context.Background(), &api.ListNodesRequest{})
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")

	raftClient := api.NewRaftMembershipClient(noCertConn)
	_, err = raftClient.Join(context.Background(), &api.JoinRequest{})
	assert.EqualError(t, err, "rpc error: code = 7 desc = Permission denied: unauthorized peer role: rpc error: code = 7 desc = no client certificates in request")

	opts = []grpc.DialOption{
		grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(managerSecurityConfig.ClientTLSCreds),
	}

	controlConn, err := grpc.Dial(tcpAddr, opts...)
	assert.NoError(t, err)
	defer func() {
		assert.NoError(t, controlConn.Close())
	}()

	// check that the kek is added to the config
	var cluster api.Cluster
	m.raftNode.MemoryStore().View(func(tx store.ReadTx) {
		clusters, err := store.FindClusters(tx, store.All)
		require.NoError(t, err)
		require.Len(t, clusters, 1)
		cluster = *clusters[0]
	})
	require.NotNil(t, cluster)
	require.Len(t, cluster.UnlockKeys, 1)
	require.Equal(t, &api.EncryptionKey{
		Subsystem: ca.ManagerRole,
		Key:       []byte("kek"),
	}, cluster.UnlockKeys[0])

	// Test removal of the agent node
	agentID := agentSecurityConfig.ClientTLSCreds.NodeID()
	assert.NoError(t, m.raftNode.MemoryStore().Update(func(tx store.Tx) error {
		return store.CreateNode(tx,
			&api.Node{
				ID: agentID,
				Certificate: api.Certificate{
					Role: api.NodeRoleWorker,
					CN:   agentID,
				},
			},
		)
	}))
	controlClient = api.NewControlClient(controlConn)
	_, err = controlClient.RemoveNode(context.Background(),
		&api.RemoveNodeRequest{
			NodeID: agentID,
			Force:  true,
		},
	)
	assert.NoError(t, err)

	client = api.NewDispatcherClient(conn)
	_, err = client.Heartbeat(context.Background(), &api.HeartbeatRequest{})
	assert.Contains(t, grpc.ErrorDesc(err), "removed from swarm")

	m.Stop(ctx)

	// After stopping we should MAY receive an error from ListenAndServe if
	// all this happened before WaitForLeader completed, so don't check the
	// error.
	<-done
}
