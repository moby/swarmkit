package ca

import (
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"net"
	"os"
	"testing"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

func TestCreateRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	_, err = CreateRootCA("rootCN", paths.RootCA)
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

func TestGetRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, err := CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	signer2, err := GetRootCA(paths.RootCA)
	assert.NoError(t, err)
	assert.Equal(t, signer, signer2)
}

func TestGenerateAndSignNewTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, err := CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	_, err = GenerateAndSignNewTLSCert(signer, "CN", "OU", paths.Manager)
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

	signer, err := CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	csr, _, err := generateNewCSR()
	assert.NoError(t, err)

	signedCert, err := ParseValidateAndSignCSR(signer, csr, "CN", "OU")
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

	signer, err := CreateRootCA("rootCN", paths.RootCA)
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

	signedCert, err := ParseValidateAndSignCSR(signer, csr, "CN", "OU")
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
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, err := CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	shaHash := sha256.New()
	shaHash.Write(signer.RootCACert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	_, err = GetRemoteCA(ctx, l.Addr().String(), mdStr)
	assert.NoError(t, err)
	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, err := CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	_, err = GetRemoteCA(ctx, l.Addr().String(), "2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b")
	assert.Error(t, err)
}
