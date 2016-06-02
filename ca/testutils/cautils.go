package testutils

import (
	"crypto/tls"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	cfsigner "github.com/cloudflare/cfssl/signer"
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
	RootCA                ca.RootCA
	MemoryStore           *store.MemoryStore
	TempDir, Organization string
	Paths                 *ca.SecurityConfigPaths
	Server                grpc.Server
	CAServer              *ca.Server
	Context               context.Context
	NodeCAClients         []api.NodeCAClient
	CAClients             []api.CAClient
	Conns                 []*grpc.ClientConn
	Picker                *picker.Picker
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
	return genSecurityConfig(tc.RootCA, role, tc.Organization, tc.TempDir)
}

// WriteNewNodeConfig returns security config for a new node, given a role
// saving the generated key and certificates to disk
func (tc *TestCA) WriteNewNodeConfig(role string) (*ca.SecurityConfig, error) {
	return genSecurityConfig(tc.RootCA, role, tc.Organization, tc.TempDir)
}

// NewNodeConfigOrg returns security config for a new node, given a role and an org
func (tc *TestCA) NewNodeConfigOrg(role, org string) (*ca.SecurityConfig, error) {
	return genSecurityConfig(tc.RootCA, role, org, tc.TempDir)
}

// WriteNewNodeConfigOrg returns security config for a new node, given a role and an org
// saving the generated key and certificates to disk
func (tc *TestCA) WriteNewNodeConfigOrg(role, org string) (*ca.SecurityConfig, error) {
	return genSecurityConfig(tc.RootCA, role, org, tc.TempDir)
}

// NewTestCA is a helper method that creates a TestCA and a bunch of default
// connections and security configs
func NewTestCA(t *testing.T, policy api.AcceptancePolicy) *TestCA {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)

	paths := ca.NewConfigPaths(tempBaseDir)
	organization := identity.NewID()

	rootCA, err := ca.CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)

	managerConfig, err := genSecurityConfig(rootCA, ca.ManagerRole, organization, "")
	assert.NoError(t, err)

	managerDiffOrgConfig, err := genSecurityConfig(rootCA, ca.ManagerRole, "swarm-test-org-2", "")
	assert.NoError(t, err)

	agentConfig, err := genSecurityConfig(rootCA, ca.AgentRole, organization, "")
	assert.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	baseOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	insecureClientOpts := append(baseOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	clientOpts := append(baseOpts, grpc.WithTransportCredentials(agentConfig.ClientTLSCreds))
	managerOpts := append(baseOpts, grpc.WithTransportCredentials(managerConfig.ClientTLSCreds))
	managerDiffOrgOpts := append(baseOpts, grpc.WithTransportCredentials(managerDiffOrgConfig.ClientTLSCreds))

	conn1, err := grpc.Dial(l.Addr().String(), insecureClientOpts...)
	assert.NoError(t, err)

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts...)
	assert.NoError(t, err)

	conn3, err := grpc.Dial(l.Addr().String(), managerOpts...)
	assert.NoError(t, err)

	conn4, err := grpc.Dial(l.Addr().String(), managerDiffOrgOpts...)
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

	remotes := picker.NewRemotes(api.Peer{Addr: l.Addr().String()})
	picker := picker.NewPicker(remotes, l.Addr().String())

	caClients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2), api.NewCAClient(conn3)}
	nodeCAClients := []api.NodeCAClient{api.NewNodeCAClient(conn1), api.NewNodeCAClient(conn2), api.NewNodeCAClient(conn3), api.NewNodeCAClient(conn4)}
	conns := []*grpc.ClientConn{conn1, conn2, conn3, conn4}

	return &TestCA{
		RootCA:        rootCA,
		MemoryStore:   s,
		Picker:        picker,
		TempDir:       tempBaseDir,
		Organization:  organization,
		Paths:         paths,
		Context:       ctx,
		CAClients:     caClients,
		NodeCAClients: nodeCAClients,
		Conns:         conns,
		CAServer:      caServer,
	}
}

func genSecurityConfig(rootCA ca.RootCA, role, org, tmpDir string) (*ca.SecurityConfig, error) {
	req := &cfcsr.CertificateRequest{
		KeyRequest: cfcsr.NewBasicKeyRequest(),
	}

	csr, key, err := cfcsr.ParseRequest(req)
	if err != nil {
		return nil, err
	}

	// Obtain a signed Certificate
	nodeID := identity.NewNodeID()
	// All managers get added the subject-alt-name of CA, so they can be used for cert issuance
	hosts := []string{role}
	if role == ca.ManagerRole {
		hosts = append(hosts, ca.CARole)
	}

	cert, err := rootCA.Signer.Sign(cfsigner.SignRequest{
		Request: string(csr),
		// OU is used for Authentication of the node type. The CN has the random
		// node ID.
		Subject: &cfsigner.Subject{CN: nodeID, Names: []cfcsr.Name{{OU: role, O: org}}},
		// Adding ou as DNS alt name, so clients can connect to ManagerRole and CARole
		Hosts: hosts,
	})
	if err != nil {
		return nil, err
	}

	// Append the root CA Key to the certificate, to create a valid chain
	certChain := append(cert, rootCA.Cert...)

	// If we were instructed to persist the files
	if tmpDir != "" {
		paths := ca.NewConfigPaths(tmpDir)
		if err := ioutil.WriteFile(paths.Node.Cert, certChain, 0644); err != nil {
			return nil, err
		}
		if err := ioutil.WriteFile(paths.Node.Key, key, 0600); err != nil {
			return nil, err
		}
	}

	// Load a valid tls.Certificate from the chain and the key
	nodeCert, err := tls.X509KeyPair(certChain, key)
	if err != nil {
		return nil, err
	}

	nodeServerTLSCreds, err := rootCA.NewServerTLSCredentials(&nodeCert)
	if err != nil {
		return nil, err
	}

	nodeClientTLSCreds, err := rootCA.NewClientTLSCredentials(&nodeCert, ca.ManagerRole)
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
