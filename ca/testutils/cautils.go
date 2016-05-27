package testutils

import (
	"crypto/tls"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/docker/swarm-v2/picker"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// AutoAcceptPolicy is a policy that autoaccepts both Managers and Agents
func AutoAcceptPolicy() api.AcceptancePolicy {
	return api.AcceptancePolicy{
		Autoaccept: map[string]bool{ca.AgentRole: true, ca.ManagerRole: true},
	}
}

// TestCA is a structure that encapsulates everything needed to test a CA Server
type TestCA struct {
	RootCA        ca.RootCA
	MemoryStore   *store.MemoryStore
	TempDir       string
	Paths         *ca.SecurityConfigPaths
	Server        grpc.Server
	CAServer      *ca.Server
	Context       context.Context
	NodeCAClients []api.NodeCAClient
	CAClients     []api.CAClient
	Conns         []*grpc.ClientConn
	Picker        *picker.Picker
}

// Stop cleansup after TestCA
func (tc *TestCA) Stop() {
	os.RemoveAll(tc.TempDir)
	for _, conn := range tc.Conns {
		conn.Close()
	}
	tc.CAServer.Stop()
	tc.Server.Stop()
}

// NewNodeConfig returns security config for a new node, given a role
func (tc *TestCA) NewNodeConfig(role string) (*ca.SecurityConfig, error) {
	nodeID := identity.NewNodeID()
	nodeCert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.Paths.Node, nodeID, role)
	if err != nil {
		return nil, err
	}

	nodeServerTLSCreds, err := tc.RootCA.NewServerTLSCredentials(nodeCert)
	if err != nil {
		return nil, err
	}

	nodeClientTLSCreds, err := tc.RootCA.NewClientTLSCredentials(nodeCert, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return ca.NewSecurityConfig(&tc.RootCA, nodeClientTLSCreds, nodeServerTLSCreds), nil
}

// NewTestCA is a helper method that creates a TestCA and a bunch of default
// connections and security configs
func NewTestCA(t *testing.T, policy api.AcceptancePolicy) *TestCA {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)

	managerConfig, err := genSecurityConfig(rootCA, tempBaseDir, ca.ManagerRole)
	assert.NoError(t, err)

	agentConfig, err := genSecurityConfig(rootCA, tempBaseDir, ca.AgentRole)
	assert.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	baseOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	insecureClientOpts := append(baseOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	clientOpts := append(baseOpts, grpc.WithTransportCredentials(agentConfig.ClientTLSCreds))
	managerOpts := append(baseOpts, grpc.WithTransportCredentials(managerConfig.ClientTLSCreds))

	conn1, err := grpc.Dial(l.Addr().String(), insecureClientOpts...)
	assert.NoError(t, err)

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts...)
	assert.NoError(t, err)

	conn3, err := grpc.Dial(l.Addr().String(), managerOpts...)
	assert.NoError(t, err)

	serverOpts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(serverOpts...)

	s := store.NewMemoryStore(nil)
	createClusterObject(t, s, policy)
	caServer := ca.NewServer(s, managerConfig)
	api.RegisterCAServer(grpcServer, caServer)
	api.RegisterNodeCAServer(grpcServer, caServer)

	ctx := context.Background()

	go func() {
		grpcServer.Serve(l)
	}()
	go func() {
		caServer.Run(ctx)
	}()

	remotes := picker.NewRemotes(l.Addr().String())
	picker := picker.NewPicker(l.Addr().String(), remotes)

	caClients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2), api.NewCAClient(conn3)}
	nodeCAClients := []api.NodeCAClient{api.NewNodeCAClient(conn1), api.NewNodeCAClient(conn2), api.NewNodeCAClient(conn3)}
	conns := []*grpc.ClientConn{conn1, conn2, conn3}

	return &TestCA{
		RootCA:        rootCA,
		MemoryStore:   s,
		Picker:        picker,
		TempDir:       tempBaseDir,
		Paths:         paths,
		Context:       ctx,
		CAClients:     caClients,
		NodeCAClients: nodeCAClients,
		Conns:         conns,
		CAServer:      caServer,
	}
}

func genSecurityConfig(rootCA ca.RootCA, tempBaseDir, role string) (*ca.SecurityConfig, error) {
	paths := ca.NewConfigPaths(tempBaseDir)

	nodeID := identity.NewNodeID()
	nodeCert, err := ca.GenerateAndSignNewTLSCert(rootCA, nodeID, role, paths.Node)
	if err != nil {
		return nil, err
	}

	nodeServerTLSCreds, err := rootCA.NewServerTLSCredentials(nodeCert)
	if err != nil {
		return nil, err
	}

	nodeClientTLSCreds, err := rootCA.NewClientTLSCredentials(nodeCert, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return ca.NewSecurityConfig(&rootCA, nodeClientTLSCreds, nodeServerTLSCreds), nil
}

func createClusterObject(t *testing.T, s *store.MemoryStore, acceptancePolicy api.AcceptancePolicy) {
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			ID: identity.NewID(),
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
				AcceptancePolicy: acceptancePolicy,
			},
		})
		return nil
	}))
}
