package testutils

import (
	"crypto/tls"
	"crypto/x509"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	"github.com/cloudflare/cfssl/log"
	cfsigner "github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/connectionbroker"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/ioutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/remotes"
	"github.com/opencontainers/go-digest"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestCA is a structure that encapsulates everything needed to test a CA Server
type TestCA struct {
	RootCA                ca.RootCA
	ExternalSigningServer *ExternalSigningServer
	MemoryStore           *store.MemoryStore
	TempDir, Organization string
	Paths                 *ca.SecurityConfigPaths
	Server                *grpc.Server
	CAServer              *ca.Server
	Context               context.Context
	NodeCAClients         []api.NodeCAClient
	CAClients             []api.CAClient
	Conns                 []*grpc.ClientConn
	WorkerToken           string
	ManagerToken          string
	ConnBroker            *connectionbroker.Broker
	KeyReadWriter         *ca.KeyReadWriter
}

// Stop cleansup after TestCA
func (tc *TestCA) Stop() {
	os.RemoveAll(tc.TempDir)
	for _, conn := range tc.Conns {
		conn.Close()
	}
	if tc.ExternalSigningServer != nil {
		tc.ExternalSigningServer.Stop()
	}
	tc.CAServer.Stop()
	tc.Server.Stop()
	tc.MemoryStore.Close()
}

// NewNodeConfig returns security config for a new node, given a role
func (tc *TestCA) NewNodeConfig(role string) (*ca.SecurityConfig, error) {
	withNonSigningRoot := tc.ExternalSigningServer != nil
	return genSecurityConfig(tc.MemoryStore, tc.RootCA, tc.KeyReadWriter, role, tc.Organization, tc.TempDir, withNonSigningRoot)
}

// WriteNewNodeConfig returns security config for a new node, given a role
// saving the generated key and certificates to disk
func (tc *TestCA) WriteNewNodeConfig(role string) (*ca.SecurityConfig, error) {
	withNonSigningRoot := tc.ExternalSigningServer != nil
	return genSecurityConfig(tc.MemoryStore, tc.RootCA, tc.KeyReadWriter, role, tc.Organization, tc.TempDir, withNonSigningRoot)
}

// NewNodeConfigOrg returns security config for a new node, given a role and an org
func (tc *TestCA) NewNodeConfigOrg(role, org string) (*ca.SecurityConfig, error) {
	withNonSigningRoot := tc.ExternalSigningServer != nil
	return genSecurityConfig(tc.MemoryStore, tc.RootCA, tc.KeyReadWriter, role, org, tc.TempDir, withNonSigningRoot)
}

// WriteNewNodeConfigOrg returns security config for a new node, given a role and an org
// saving the generated key and certificates to disk
func (tc *TestCA) WriteNewNodeConfigOrg(role, org string) (*ca.SecurityConfig, error) {
	withNonSigningRoot := tc.ExternalSigningServer != nil
	return genSecurityConfig(tc.MemoryStore, tc.RootCA, tc.KeyReadWriter, role, org, tc.TempDir, withNonSigningRoot)
}

// External controls whether or not NewTestCA() will create a TestCA server
// configured to use an external signer or not.
var External bool

// NewTestCA is a helper method that creates a TestCA and a bunch of default
// connections and security configs.
func NewTestCA(t *testing.T, krwGenerators ...func(ca.CertPaths) *ca.KeyReadWriter) *TestCA {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)

	s := store.NewMemoryStore(nil)

	paths := ca.NewConfigPaths(tempBaseDir)
	organization := identity.NewID()

	rootCA, err := createAndWriteRootCA("swarm-test-CA", paths.RootCA, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)

	var (
		externalSigningServer *ExternalSigningServer
		externalCAs           []*api.ExternalCA
	)

	if External {
		// Start the CA API server.
		externalSigningServer, err = NewExternalSigningServer(rootCA, tempBaseDir)
		assert.NoError(t, err)
		externalCAs = []*api.ExternalCA{
			{
				Protocol: api.ExternalCA_CAProtocolCFSSL,
				URL:      externalSigningServer.URL,
			},
		}
	}

	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)
	if len(krwGenerators) > 0 {
		krw = krwGenerators[0](paths.Node)
	}

	managerConfig, err := genSecurityConfig(s, rootCA, krw, ca.ManagerRole, organization, "", External)
	assert.NoError(t, err)

	managerDiffOrgConfig, err := genSecurityConfig(s, rootCA, krw, ca.ManagerRole, "swarm-test-org-2", "", External)
	assert.NoError(t, err)

	workerConfig, err := genSecurityConfig(s, rootCA, krw, ca.WorkerRole, organization, "", External)
	assert.NoError(t, err)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	baseOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	insecureClientOpts := append(baseOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	clientOpts := append(baseOpts, grpc.WithTransportCredentials(workerConfig.ClientTLSCreds))
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

	managerToken := ca.GenerateJoinToken(&rootCA)
	workerToken := ca.GenerateJoinToken(&rootCA)

	createClusterObject(t, s, organization, workerToken, managerToken, externalCAs...)
	caServer := ca.NewServer(s, managerConfig)
	caServer.SetReconciliationRetryInterval(50 * time.Millisecond)
	api.RegisterCAServer(grpcServer, caServer)
	api.RegisterNodeCAServer(grpcServer, caServer)

	ctx := context.Background()

	go grpcServer.Serve(l)
	go caServer.Run(ctx)

	// Wait for caServer to be ready to serve
	<-caServer.Ready()
	remotes := remotes.NewRemotes(api.Peer{Addr: l.Addr().String()})

	caClients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2), api.NewCAClient(conn3)}
	nodeCAClients := []api.NodeCAClient{api.NewNodeCAClient(conn1), api.NewNodeCAClient(conn2), api.NewNodeCAClient(conn3), api.NewNodeCAClient(conn4)}
	conns := []*grpc.ClientConn{conn1, conn2, conn3, conn4}

	return &TestCA{
		RootCA:                rootCA,
		ExternalSigningServer: externalSigningServer,
		MemoryStore:           s,
		TempDir:               tempBaseDir,
		Organization:          organization,
		Paths:                 paths,
		Context:               ctx,
		CAClients:             caClients,
		NodeCAClients:         nodeCAClients,
		Conns:                 conns,
		Server:                grpcServer,
		CAServer:              caServer,
		WorkerToken:           workerToken,
		ManagerToken:          managerToken,
		ConnBroker:            connectionbroker.New(remotes),
		KeyReadWriter:         krw,
	}
}

func createNode(s *store.MemoryStore, nodeID, role string, csr, cert []byte) error {
	apiRole, _ := ca.FormatRole(role)

	err := s.Update(func(tx store.Tx) error {
		node := &api.Node{
			ID: nodeID,
			Certificate: api.Certificate{
				CSR:  csr,
				CN:   nodeID,
				Role: apiRole,
				Status: api.IssuanceStatus{
					State: api.IssuanceStateIssued,
				},
				Certificate: cert,
			},
			Spec: api.NodeSpec{
				DesiredRole: apiRole,
				Membership:  api.NodeMembershipAccepted,
			},
			Role: apiRole,
		}

		return store.CreateNode(tx, node)
	})

	return err
}

func genSecurityConfig(s *store.MemoryStore, rootCA ca.RootCA, krw *ca.KeyReadWriter, role, org, tmpDir string, nonSigningRoot bool) (*ca.SecurityConfig, error) {
	req := &cfcsr.CertificateRequest{
		KeyRequest: cfcsr.NewBasicKeyRequest(),
	}

	csr, key, err := cfcsr.ParseRequest(req)
	if err != nil {
		return nil, err
	}

	// Obtain a signed Certificate
	nodeID := identity.NewID()
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

	err = createNode(s, nodeID, role, csr, cert)
	if err != nil {
		return nil, err
	}

	if nonSigningRoot {
		rootCA = ca.RootCA{
			Cert:   rootCA.Cert,
			Digest: rootCA.Digest,
			Pool:   rootCA.Pool,
		}
	}

	return ca.NewSecurityConfig(&rootCA, krw, nodeClientTLSCreds, nodeServerTLSCreds), nil
}

func createClusterObject(t *testing.T, s *store.MemoryStore, clusterID, workerToken, managerToken string, externalCAs ...*api.ExternalCA) {
	assert.NoError(t, s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, &api.Cluster{
			ID: clusterID,
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
				CAConfig: api.CAConfig{
					ExternalCAs: externalCAs,
				},
			},
			RootCA: api.RootCA{
				JoinTokens: api.JoinTokens{
					Worker:  workerToken,
					Manager: managerToken,
				},
			},
		})
		return nil
	}))
}

// CreateRootCertAndKey returns a generated certificate and key for a root CA
func CreateRootCertAndKey(rootCN string) ([]byte, []byte, error) {
	// Create a simple CSR for the CA using the default CA validator and policy
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: cfcsr.NewBasicKeyRequest(),
		CA:         &cfcsr.CAConfig{Expiry: ca.RootCAExpiration},
	}

	// Generate the CA and get the certificate and private key
	cert, _, key, err := initca.New(&req)
	return cert, key, err
}

// createAndWriteRootCA creates a Certificate authority for a new Swarm Cluster.
// We're copying ca.CreateRootCA, so we can have smaller key-sizes for tests
func createAndWriteRootCA(rootCN string, paths ca.CertPaths, expiry time.Duration) (ca.RootCA, error) {
	cert, key, err := CreateRootCertAndKey(rootCN)
	if err != nil {
		return ca.RootCA{}, err
	}

	// Convert the key given by initca to an object to create a ca.RootCA
	parsedKey, err := helpers.ParsePrivateKeyPEM(key)
	if err != nil {
		log.Errorf("failed to parse private key: %v", err)
		return ca.RootCA{}, err
	}

	// Convert the certificate into an object to create a ca.RootCA
	parsedCert, err := helpers.ParseCertificatePEM(cert)
	if err != nil {
		return ca.RootCA{}, err
	}

	// Create a Signer out of the private key
	signer, err := local.NewSigner(parsedKey, parsedCert, cfsigner.DefaultSigAlgo(parsedKey), ca.SigningPolicy(expiry))
	if err != nil {
		log.Errorf("failed to create signer: %v", err)
		return ca.RootCA{}, err
	}

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(paths.Cert), 0755)
	if err != nil {
		return ca.RootCA{}, err
	}

	// Write the Private Key and Certificate to disk, using decent permissions
	if err := ioutils.AtomicWriteFile(paths.Cert, cert, 0644); err != nil {
		return ca.RootCA{}, err
	}
	if err := ioutils.AtomicWriteFile(paths.Key, key, 0600); err != nil {
		return ca.RootCA{}, err
	}

	// Create a Pool with our Root CA Certificate
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		return ca.RootCA{}, errors.New("failed to append certificate to cert pool")
	}

	return ca.RootCA{
		Signer: signer,
		Key:    key,
		Cert:   cert,
		Pool:   pool,
		Digest: digest.FromBytes(cert),
	}, nil
}
