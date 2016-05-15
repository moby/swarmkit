package ca

import (
	"crypto/sha256"
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
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func AutoAcceptPolicy() api.AcceptancePolicy {
	return api.AcceptancePolicy{
		Autoaccept: map[string]bool{AgentRole: true, ManagerRole: true},
	}
}

type TestService struct {
	rootCA       RootCA
	s            *store.MemoryStore
	addr, tmpDir string
	paths        *SecurityConfigPaths
	server       grpc.Server
	caServer     grpc.Server
	ctx          context.Context
}

func NewTestService(t *testing.T, policy api.AcceptancePolicy) *TestService {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
	assert.NoError(t, err)

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	s := store.NewMemoryStore(nil)
	caserver := NewServer(s, managerConfig, policy)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	go func() {
		grpcServer.Serve(l)
	}()
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()

	return &TestService{
		rootCA: rootCA,
		s:      s,
		addr:   l.Addr().String(),
		tmpDir: tempBaseDir,
		paths:  paths,
		ctx:    context.Background(),
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
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	shaHash := sha256.New()
	shaHash.Write(ts.rootCA.Cert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	cert, err := GetRemoteCA(ts.ctx, ts.addr, mdStr)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestCanSign(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	assert.True(t, ts.rootCA.CanSign())
	ts.rootCA.Signer = nil
	assert.False(t, ts.rootCA.CanSign())
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	_, err := GetRemoteCA(ts.ctx, ts.addr, "2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b")
	assert.Error(t, err)
}

func TestIssueAndSaveNewCertificates(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Copy the current RootCA without the signer
	rca := RootCA{Cert: ts.rootCA.Cert, Pool: ts.rootCA.Pool}
	cert, err := rca.IssueAndSaveNewCertificates(ts.ctx, ts.paths.Manager, ManagerRole, ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(ts.paths.Manager.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
}

func TestGetRemoteSignedCertificateAutoAccept(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(ts.paths.Manager)
	assert.NoError(t, err)

	certs, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.addr, ts.rootCA.Pool)
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ManagerRole)

	certs, err = getRemoteSignedCertificate(ts.ctx, csr, AgentRole, ts.addr, ts.rootCA.Pool)
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], AgentRole)

}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	ts := NewTestService(t, DefaultAcceptancePolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(ts.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(ts.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.addr, ts.rootCA.Pool)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = ts.s.Update(func(tx store.Tx) error {
		regCert.Status.State = api.IssuanceStateIssued

		return store.UpdateRegisteredCertificate(tx, regCert)
	})
	assert.NoError(t, err)

	// Make sure getRemoteSignedCertificate didn't return an error
	assert.NoError(t, <-completed)
}

func TestGetRemoteSignedCertificateRejected(t *testing.T) {
	ts := NewTestService(t, DefaultAcceptancePolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(ts.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(ts.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.addr, ts.rootCA.Pool)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = ts.s.Update(func(tx store.Tx) error {
		regCert.Status.State = api.IssuanceStateRejected

		return store.UpdateRegisteredCertificate(tx, regCert)
	})
	assert.NoError(t, err)

	// Make sure getRemoteSignedCertificate didn't return an error
	assert.EqualError(t, <-completed, "certificate issuance rejected: ISSUANCE_REJECTED")
}

func TestGetRemoteSignedCertificateBlocked(t *testing.T) {
	ts := NewTestService(t, DefaultAcceptancePolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Create a new CSR to be signed
	csr, _, err := GenerateAndWriteNewCSR(ts.paths.Manager)
	assert.NoError(t, err)

	updates, cancel := state.Watch(ts.s.WatchQueue(), state.EventCreateRegisteredCertificate{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := getRemoteSignedCertificate(context.Background(), csr, ManagerRole, ts.addr, ts.rootCA.Pool)
		completed <- err
	}()

	event := <-updates
	regCert := event.(state.EventCreateRegisteredCertificate).RegisteredCertificate.Copy()

	// Directly update the status of the store
	err = ts.s.Update(func(tx store.Tx) error {
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
