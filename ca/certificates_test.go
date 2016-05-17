package ca

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/docker/swarm-v2/picker"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

func AutoAcceptPolicy() api.AcceptancePolicy {
	return api.AcceptancePolicy{
		Autoaccept: map[string]bool{AgentRole: true, ManagerRole: true},
	}
}

type TestCA struct {
	rootCA   RootCA
	s        *store.MemoryStore
	tmpDir   string
	paths    *SecurityConfigPaths
	server   grpc.Server
	caServer *Server
	ctx      context.Context
	clients  []api.CAClient
	conns    []*grpc.ClientConn
	picker   *picker.Picker
}

func (tc *TestCA) Stop() {
	os.RemoveAll(tc.tmpDir)
	for _, conn := range tc.conns {
		conn.Close()
	}
	tc.caServer.Stop()
	tc.server.Stop()
}

func NewTestCA(t *testing.T, policy api.AcceptancePolicy) *TestCA {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)

	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
	assert.NoError(t, err)

	agentConfig, err := genAgentSecurityConfig(rootCA, tempBaseDir)
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
	caServer := NewServer(s, managerConfig)
	api.RegisterCAServer(grpcServer, caServer)

	go func() {
		grpcServer.Serve(l)
	}()
	go func() {
		assert.NoError(t, caServer.Run(context.Background()))
	}()

	remotes := picker.NewRemotes(l.Addr().String())
	picker := picker.NewPicker(l.Addr().String(), remotes)

	clients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2), api.NewCAClient(conn3)}
	conns := []*grpc.ClientConn{conn1, conn2, conn3}

	return &TestCA{
		rootCA:   rootCA,
		s:        s,
		picker:   picker,
		tmpDir:   tempBaseDir,
		paths:    paths,
		ctx:      context.Background(),
		clients:  clients,
		conns:    conns,
		caServer: caServer,
	}
}

func TestCreateAndWriteRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	_, err = CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.RootCA.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.RootCA.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestCreateAndWriteRootCAExpiry(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	// Convert the certificate into an object to create a RootCA
	parsedCert, err := helpers.ParseCertificatePEM(rootCA.Cert)
	assert.NoError(t, err)
	assert.True(t, time.Now().Add(time.Hour*24*364*20).Before(parsedCert.NotAfter))

}

func TestGetLocalRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	rootCA2, err := GetLocalRootCA(paths.RootCA)
	assert.NoError(t, err)
	assert.Equal(t, rootCA, rootCA2)
}

func TestGenerateAndSignNewTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	_, err = GenerateAndSignNewTLSCert(rootCA, "CN", "OU", paths.Manager)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.Manager.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Manager.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestGenerateAndWriteNewCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	csr, key, err := GenerateAndWriteNewCSR(paths.Manager)
	assert.NoError(t, err)
	assert.NotNil(t, csr)
	assert.NotNil(t, key)

	perms, err := permbits.Stat(paths.Manager.CSR)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Manager.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())

	_, err = helpers.ParseCSRPEM(csr)
	assert.NoError(t, err)
}

func TestParseValidateAndSignCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	csr, _, err := generateNewCSR()
	assert.NoError(t, err)

	signedCert, err := rootCA.ParseValidateAndSignCSR(csr, "CN", "OU")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 2, len(parsedCert[0].Subject.Names))
}

func TestParseValidateAndSignMaliciousCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	req := &cfcsr.CertificateRequest{
		Names: []cfcsr.Name{
			{
				O:  "maliciousOrg",
				OU: "maliciousOu",
			},
		},
		CN:         "maliciousCN",
		Hosts:      []string{"docker.com"},
		KeyRequest: &cfcsr.BasicKeyRequest{A: "ecdsa", S: 256},
	}

	csr, _, err := cfcsr.ParseRequest(req)
	assert.NoError(t, err)

	signedCert, err := rootCA.ParseValidateAndSignCSR(csr, "CN", "OU")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 2, len(parsedCert[0].Subject.Names))
	assert.Empty(t, "", parsedCert[0].Subject.Organization)
}

func TestGetRemoteCA(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	shaHash := sha256.New()
	shaHash.Write(tc.rootCA.Cert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	cert, err := GetRemoteCA(ts.ctx, mdStr, ts.picker)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestCanSign(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	assert.True(t, tc.rootCA.CanSign())
	tc.rootCA.Signer = nil
	assert.False(t, tc.rootCA.CanSign())
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	_, err := GetRemoteCA(ts.ctx, "2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b", ts.picker)
	assert.Error(t, err)
}

func TestIssueAndSaveNewCertificates(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Copy the current RootCA without the signer
	rca := RootCA{Cert: ts.rootCA.Cert, Pool: ts.rootCA.Pool}
	cert, err := rca.IssueAndSaveNewCertificates(ts.ctx, ts.paths.Manager, ManagerRole, ts.picker)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.paths.Manager.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
}

func TestGetRemoteSignedCertificateAutoAccept(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)

	certs, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.rootCA.Pool, ts.picker)
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ManagerRole)

	certs, err = getRemoteSignedCertificate(ts.ctx, csr, AgentRole, ts.rootCA.Pool, ts.picker)
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], AgentRole)

}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.rootCA.Pool, ts.picker)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = tc.s.Update(func(tx store.Tx) error {
		regCert.Status.State = api.IssuanceStateIssued

		return store.UpdateRegisteredCertificate(tx, regCert)
	})
	assert.NoError(t, err)

	// Make sure getRemoteSignedCertificate didn't return an error
	assert.NoError(t, <-completed)
}

func TestGetRemoteSignedCertificateRejected(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.rootCA.Pool, ts.picker)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = tc.s.Update(func(tx store.Tx) error {
		regCert.Status.State = api.IssuanceStateRejected

		return store.UpdateRegisteredCertificate(tx, regCert)
	})
	assert.NoError(t, err)

	// Make sure getRemoteSignedCertificate didn't return an error
	assert.EqualError(t, <-completed, "certificate issuance rejected: ISSUANCE_REJECTED")
}

func TestGetRemoteSignedCertificateBlocked(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.rootCA.Pool, ts.picker)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = tc.s.Update(func(tx store.Tx) error {
		regCert.Status.State = api.IssuanceStateBlocked

		return store.UpdateRegisteredCertificate(tx, regCert)
	})
	assert.NoError(t, err)

	// Make sure getRemoteSignedCertificate didn't return an error
	assert.EqualError(t, <-completed, "certificate issuance rejected: ISSUANCE_BLOCKED")
}

func genManagerSecurityConfig(rootCA RootCA, tempBaseDir string) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	managerID := identity.NewID()
	managerCert, err := GenerateAndSignNewTLSCert(rootCA, managerID, ManagerRole, paths.Manager)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := rootCA.NewServerTLSCredentials(managerCert)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := rootCA.NewClientTLSCredentials(managerCert, ManagerRole)
	if err != nil {
		return nil, err
	}

	ManagerSecurityConfig := &ManagerSecurityConfig{}
	ManagerSecurityConfig.RootCA = rootCA
	ManagerSecurityConfig.ServerTLSCreds = managerTLSCreds
	ManagerSecurityConfig.ClientTLSCreds = managerClientTLSCreds

	return ManagerSecurityConfig, nil
}

func genAgentSecurityConfig(rootCA RootCA, tempBaseDir string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	agentID := identity.NewID()
	agentCert, err := GenerateAndSignNewTLSCert(rootCA, agentID, AgentRole, paths.Agent)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := rootCA.NewClientTLSCredentials(agentCert, ManagerRole)
	if err != nil {
		return nil, err
	}

	AgentSecurityConfig := &AgentSecurityConfig{}
	AgentSecurityConfig.RootCA = rootCA
	AgentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

	return AgentSecurityConfig, nil
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
