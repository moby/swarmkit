package testutils

import (
	"context"
	"crypto"
	cryptorand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/ca/pkcs8"
	"github.com/moby/swarmkit/v2/connectionbroker"
	"github.com/moby/swarmkit/v2/identity"
	"github.com/moby/swarmkit/v2/ioutils"
	"github.com/moby/swarmkit/v2/log"
	"github.com/moby/swarmkit/v2/manager/state/store"
	stateutils "github.com/moby/swarmkit/v2/manager/state/testutils"
	"github.com/moby/swarmkit/v2/remotes"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// TestCA is a structure that encapsulates everything needed to test a CA Server
type TestCA struct {
	RootCA                      ca.RootCA
	ExternalSigningServer       *ExternalSigningServer
	MemoryStore                 *store.MemoryStore
	Addr, TempDir, Organization string
	Paths                       *ca.SecurityConfigPaths
	Server                      *grpc.Server
	ServingSecurityConfig       *ca.SecurityConfig
	CAServer                    *ca.Server
	Context                     context.Context
	NodeCAClients               []api.NodeCAClient
	CAClients                   []api.CAClient
	Conns                       []*grpc.ClientConn
	WorkerToken                 string
	ManagerToken                string
	ConnBroker                  *connectionbroker.Broker
	KeyReadWriter               *ca.KeyReadWriter
	ctxCancel                   func()
	securityConfigCleanups      []func() error
}

// Stop cleans up after TestCA
func (tc *TestCA) Stop() {
	tc.ctxCancel()
	for _, qClose := range tc.securityConfigCleanups {
		qClose()
	}
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
	return tc.NewNodeConfigOrg(role, tc.Organization)
}

// WriteNewNodeConfig returns security config for a new node, given a role
// saving the generated key and certificates to disk
func (tc *TestCA) WriteNewNodeConfig(role string) (*ca.SecurityConfig, error) {
	return tc.NewNodeConfigOrg(role, tc.Organization)
}

// NewNodeConfigOrg returns security config for a new node, given a role and an org
func (tc *TestCA) NewNodeConfigOrg(role, org string) (*ca.SecurityConfig, error) {
	withNonSigningRoot := tc.ExternalSigningServer != nil
	s, qClose, err := genSecurityConfig(tc.MemoryStore, tc.RootCA, tc.KeyReadWriter, role, org, tc.TempDir, withNonSigningRoot)
	if err != nil {
		tc.securityConfigCleanups = append(tc.securityConfigCleanups, qClose)
	}
	return s, err
}

// External controls whether or not NewTestCA() will create a TestCA server
// configured to use an external signer or not.
var External bool

// NewTestCA is a helper method that creates a TestCA and a bunch of default
// connections and security configs.
func NewTestCA(t *testing.T, krwGenerators ...func(ca.CertPaths) *ca.KeyReadWriter) *TestCA {
	tempdir, err := os.MkdirTemp("", "swarm-ca-test-")
	if t != nil {
		require.NoError(t, err)
	}

	cert, key, err := CreateRootCertAndKey("swarm-test-CA")
	if t != nil {
		require.NoError(t, err)
	}
	apiRootCA := api.RootCA{
		CACert: cert,
		CAKey:  key,
	}

	return newTestCA(t, tempdir, apiRootCA, krwGenerators, false)
}

// NewFIPSTestCA is a helper method that creates a mandatory fips TestCA and a bunch of default
// connections and security configs.
func NewFIPSTestCA(t *testing.T) *TestCA {
	tempdir, err := os.MkdirTemp("", "swarm-ca-test-")
	if t != nil {
		require.NoError(t, err)
	}

	cert, key, err := CreateRootCertAndKey("swarm-test-CA")
	if t != nil {
		require.NoError(t, err)
	}
	apiRootCA := api.RootCA{
		CACert: cert,
		CAKey:  key,
	}

	return newTestCA(t, tempdir, apiRootCA, nil, true)
}

// NewTestCAFromAPIRootCA is a helper method that creates a TestCA and a bunch of default
// connections and security configs, given a temp directory and an api.RootCA to use for creating
// a cluster and for signing.
func NewTestCAFromAPIRootCA(t *testing.T, tempBaseDir string, apiRootCA api.RootCA, krwGenerators []func(ca.CertPaths) *ca.KeyReadWriter) *TestCA {
	return newTestCA(t, tempBaseDir, apiRootCA, krwGenerators, false)
}

func newTestCA(t *testing.T, tempBaseDir string, apiRootCA api.RootCA, krwGenerators []func(ca.CertPaths) *ca.KeyReadWriter, fips bool) *TestCA {
	s := store.NewMemoryStore(&stateutils.MockProposer{})

	paths := ca.NewConfigPaths(tempBaseDir)
	organization := identity.NewID()
	if fips {
		organization = "FIPS." + organization
	}

	var (
		externalSigningServer *ExternalSigningServer
		externalCAs           []*api.ExternalCA
		err                   error
		rootCA                ca.RootCA
	)

	if apiRootCA.RootRotation != nil {
		rootCA, err = ca.NewRootCA(
			apiRootCA.CACert, apiRootCA.RootRotation.CACert, apiRootCA.RootRotation.CAKey, ca.DefaultNodeCertExpiration, apiRootCA.RootRotation.CrossSignedCACert)
	} else {
		rootCA, err = ca.NewRootCA(
			apiRootCA.CACert, apiRootCA.CACert, apiRootCA.CAKey, ca.DefaultNodeCertExpiration, nil)

	}
	if t != nil {
		require.NoError(t, err)
	}

	// Write the root certificate to disk, using decent permissions
	err = ioutils.AtomicWriteFile(paths.RootCA.Cert, apiRootCA.CACert, 0o644)
	if t != nil {
		require.NoError(t, err)
	}

	if External {
		// Start the CA API server - ensure that the external server doesn't have any intermediates
		var extRootCA ca.RootCA
		if apiRootCA.RootRotation != nil {
			extRootCA, err = ca.NewRootCA(
				apiRootCA.RootRotation.CACert, apiRootCA.RootRotation.CACert, apiRootCA.RootRotation.CAKey, ca.DefaultNodeCertExpiration, nil)
			// remove the key from the API root CA so that once the CA server starts up, it won't have a local signer
			apiRootCA.RootRotation.CAKey = nil
		} else {
			extRootCA, err = ca.NewRootCA(
				apiRootCA.CACert, apiRootCA.CACert, apiRootCA.CAKey, ca.DefaultNodeCertExpiration, nil)
			// remove the key from the API root CA so that once the CA server starts up, it won't have a local signer
			apiRootCA.CAKey = nil
		}
		if t != nil {
			require.NoError(t, err)
		}

		externalSigningServer, err = NewExternalSigningServer(extRootCA, tempBaseDir)
		if t != nil {
			require.NoError(t, err)
		}

		externalCAs = []*api.ExternalCA{
			{
				Protocol: api.ExternalCA_CAProtocolCFSSL,
				URL:      externalSigningServer.URL,
				CACert:   extRootCA.Certs,
			},
		}
	}

	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)
	if len(krwGenerators) > 0 {
		krw = krwGenerators[0](paths.Node)
	}

	managerConfig, qClose1, err := genSecurityConfig(s, rootCA, krw, ca.ManagerRole, organization, "", External)
	if t != nil {
		assert.NoError(t, err)
	}

	managerDiffOrgConfig, qClose2, err := genSecurityConfig(s, rootCA, krw, ca.ManagerRole, "swarm-test-org-2", "", External)
	if t != nil {
		assert.NoError(t, err)
	}

	workerConfig, qClose3, err := genSecurityConfig(s, rootCA, krw, ca.WorkerRole, organization, "", External)
	if t != nil {
		assert.NoError(t, err)
	}

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if t != nil {
		assert.NoError(t, err)
	}

	baseOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	insecureClientOpts := append(baseOpts, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})))
	clientOpts := append(baseOpts, grpc.WithTransportCredentials(workerConfig.ClientTLSCreds))
	managerOpts := append(baseOpts, grpc.WithTransportCredentials(managerConfig.ClientTLSCreds))
	managerDiffOrgOpts := append(baseOpts, grpc.WithTransportCredentials(managerDiffOrgConfig.ClientTLSCreds))

	conn1, err := grpc.Dial(l.Addr().String(), insecureClientOpts...)
	if t != nil {
		assert.NoError(t, err)
	}

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts...)
	if t != nil {
		assert.NoError(t, err)
	}

	conn3, err := grpc.Dial(l.Addr().String(), managerOpts...)
	if t != nil {
		assert.NoError(t, err)
	}

	conn4, err := grpc.Dial(l.Addr().String(), managerDiffOrgOpts...)
	if t != nil {
		assert.NoError(t, err)
	}

	serverOpts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(serverOpts...)

	clusterObj := createClusterObject(t, s, organization, apiRootCA, &rootCA, externalCAs...)

	caServer := ca.NewServer(s, managerConfig)
	caServer.SetReconciliationRetryInterval(50 * time.Millisecond)
	caServer.SetRootReconciliationInterval(50 * time.Millisecond)
	api.RegisterCAServer(grpcServer, caServer)
	api.RegisterNodeCAServer(grpcServer, caServer)

	fields := logrus.Fields{"testHasExternalCA": External}
	if t != nil {
		fields["testname"] = t.Name()
	}
	ctx, ctxCancel := context.WithCancel(log.WithLogger(context.Background(), log.L.WithFields(fields)))

	go grpcServer.Serve(l)
	go caServer.Run(ctx)

	// Wait for caServer to be ready to serve
	<-caServer.Ready()
	remotes := remotes.NewRemotes(api.Peer{Addr: l.Addr().String()})

	caClients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2), api.NewCAClient(conn3)}
	nodeCAClients := []api.NodeCAClient{api.NewNodeCAClient(conn1), api.NewNodeCAClient(conn2), api.NewNodeCAClient(conn3), api.NewNodeCAClient(conn4)}
	conns := []*grpc.ClientConn{conn1, conn2, conn3, conn4}

	return &TestCA{
		RootCA:                 rootCA,
		ExternalSigningServer:  externalSigningServer,
		MemoryStore:            s,
		TempDir:                tempBaseDir,
		Organization:           organization,
		Paths:                  paths,
		Context:                ctx,
		CAClients:              caClients,
		NodeCAClients:          nodeCAClients,
		Conns:                  conns,
		Addr:                   l.Addr().String(),
		Server:                 grpcServer,
		ServingSecurityConfig:  managerConfig,
		CAServer:               caServer,
		WorkerToken:            clusterObj.RootCA.JoinTokens.Worker,
		ManagerToken:           clusterObj.RootCA.JoinTokens.Manager,
		ConnBroker:             connectionbroker.New(remotes),
		KeyReadWriter:          krw,
		ctxCancel:              ctxCancel,
		securityConfigCleanups: []func() error{qClose1, qClose2, qClose3},
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

func genSecurityConfig(s *store.MemoryStore, rootCA ca.RootCA, krw *ca.KeyReadWriter, role, org, tmpDir string, nonSigningRoot bool) (*ca.SecurityConfig, func() error, error) {
	req := &cfcsr.CertificateRequest{
		KeyRequest: cfcsr.NewKeyRequest(),
	}

	csr, key, err := cfcsr.ParseRequest(req)
	if err != nil {
		return nil, nil, err
	}

	key, err = pkcs8.ConvertECPrivateKeyPEM(key)
	if err != nil {
		return nil, nil, err
	}

	// Obtain a signed Certificate
	nodeID := identity.NewID()

	certChain, err := rootCA.ParseValidateAndSignCSR(csr, nodeID, role, org)
	if err != nil {
		return nil, nil, err
	}

	// If we were instructed to persist the files
	if tmpDir != "" {
		paths := ca.NewConfigPaths(tmpDir)
		if err := os.WriteFile(paths.Node.Cert, certChain, 0o644); err != nil {
			return nil, nil, err
		}
		if err := os.WriteFile(paths.Node.Key, key, 0o600); err != nil {
			return nil, nil, err
		}
	}

	// Load a valid tls.Certificate from the chain and the key
	nodeCert, err := tls.X509KeyPair(certChain, key)
	if err != nil {
		return nil, nil, err
	}

	err = createNode(s, nodeID, role, csr, certChain)
	if err != nil {
		return nil, nil, err
	}

	signingCert := rootCA.Certs
	if len(rootCA.Intermediates) > 0 {
		signingCert = rootCA.Intermediates
	}
	parsedCert, err := helpers.ParseCertificatePEM(signingCert)
	if err != nil {
		return nil, nil, err
	}

	if nonSigningRoot {
		rootCA = ca.RootCA{
			Certs:         rootCA.Certs,
			Digest:        rootCA.Digest,
			Pool:          rootCA.Pool,
			Intermediates: rootCA.Intermediates,
		}
	}

	return ca.NewSecurityConfig(&rootCA, krw, &nodeCert, &ca.IssuerInfo{
		PublicKey: parsedCert.RawSubjectPublicKeyInfo,
		Subject:   parsedCert.RawSubject,
	})
}

func createClusterObject(t *testing.T, s *store.MemoryStore, clusterID string, apiRootCA api.RootCA, caRootCA *ca.RootCA, externalCAs ...*api.ExternalCA) *api.Cluster {
	fips := strings.HasPrefix(clusterID, "FIPS.")
	cluster := &api.Cluster{
		ID: clusterID,
		Spec: api.ClusterSpec{
			Annotations: api.Annotations{
				Name: store.DefaultClusterName,
			},
			CAConfig: api.CAConfig{
				ExternalCAs: externalCAs,
			},
		},
		RootCA: apiRootCA,
		FIPS:   fips,
	}
	if cluster.RootCA.JoinTokens.Worker == "" {
		cluster.RootCA.JoinTokens.Worker = ca.GenerateJoinToken(caRootCA, fips)
	}
	if cluster.RootCA.JoinTokens.Manager == "" {
		cluster.RootCA.JoinTokens.Manager = ca.GenerateJoinToken(caRootCA, fips)
	}
	err := s.Update(func(tx store.Tx) error {
		store.CreateCluster(tx, cluster)
		return nil
	})
	if t != nil {
		assert.NoError(t, err)
	}
	return cluster
}

// CreateRootCertAndKey returns a generated certificate and key for a root CA
func CreateRootCertAndKey(rootCN string) ([]byte, []byte, error) {
	// Create a simple CSR for the CA using the default CA validator and policy
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: cfcsr.NewKeyRequest(),
		CA:         &cfcsr.CAConfig{Expiry: ca.RootCAExpiration},
	}

	// Generate the CA and get the certificate and private key
	cert, _, key, err := initca.New(&req)
	if err != nil {
		return nil, nil, err
	}

	key, err = pkcs8.ConvertECPrivateKeyPEM(key)
	if err != nil {
		return nil, nil, err
	}

	return cert, key, err
}

// ReDateCert takes an existing cert and changes the not before and not after date, to make it easier
// to test expiry
func ReDateCert(t *testing.T, cert, signerCert, signerKey []byte, notBefore, notAfter time.Time) []byte {
	signee, err := helpers.ParseCertificatePEM(cert)
	require.NoError(t, err)
	signer, err := helpers.ParseCertificatePEM(signerCert)
	require.NoError(t, err)
	key, err := helpers.ParsePrivateKeyPEM(signerKey)
	require.NoError(t, err)
	signee.NotBefore = notBefore
	signee.NotAfter = notAfter

	derBytes, err := x509.CreateCertificate(cryptorand.Reader, signee, signer, signee.PublicKey, key)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: derBytes,
	})
}

// CreateCertFromSigner creates a Certificate authority for a new Swarm Cluster given an existing key only.
func CreateCertFromSigner(rootCN string, priv crypto.Signer) ([]byte, error) {
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: &cfcsr.KeyRequest{A: ca.RootKeyAlgo, S: ca.RootKeySize},
		CA:         &cfcsr.CAConfig{Expiry: ca.RootCAExpiration},
	}
	cert, _, err := initca.NewFromSigner(&req, priv)
	return cert, err
}
