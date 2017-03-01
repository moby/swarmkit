package ca_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/state"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/opencontainers/go-digest"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/net/context"
)

func init() {
	os.Setenv(ca.PassphraseENVVar, "")
	os.Setenv(ca.PassphraseENVVarPrev, "")
}

func checkSingleCert(t *testing.T, certBytes []byte, issuerName, cn, ou, org string, additionalDNSNames ...string) {
	certs, err := helpers.ParseCertificatesPEM(certBytes)
	require.NoError(t, err)
	require.Len(t, certs, 1)
	require.Equal(t, issuerName, certs[0].Issuer.CommonName)
	require.Equal(t, cn, certs[0].Subject.CommonName)
	require.Equal(t, []string{ou}, certs[0].Subject.OrganizationalUnit)
	require.Equal(t, []string{org}, certs[0].Subject.Organization)

	require.Len(t, certs[0].DNSNames, len(additionalDNSNames)+2)
	for _, dnsName := range append(additionalDNSNames, cn, ou) {
		require.Contains(t, certs[0].DNSNames, dnsName)
	}
}

// TestMain runs every test in this file twice - once with a local CA and
// again with an external CA server.
func TestMain(m *testing.M) {
	if status := m.Run(); status != 0 {
		os.Exit(status)
	}

	testutils.External = true
	os.Exit(m.Run())
}

func TestCreateRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	_, err = ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.RootCA.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())

	_, err = permbits.Stat(paths.RootCA.Key)
	assert.True(t, os.IsNotExist(err))
}

func TestCreateRootCAExpiry(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	// Convert the certificate into an object to create a RootCA
	parsedCert, err := helpers.ParseCertificatePEM(rootCA.Cert)
	assert.NoError(t, err)
	duration, err := time.ParseDuration(ca.RootCAExpiration)
	assert.NoError(t, err)
	assert.True(t, time.Now().Add(duration).AddDate(0, -1, 0).Before(parsedCert.NotAfter))

}

func TestGetLocalRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	// First, try to load the local Root CA with the certificate missing.
	_, err = ca.GetLocalRootCA(paths.RootCA)
	assert.Equal(t, ca.ErrNoLocalRootCA, err)

	// Create the local Root CA to ensure that we can reload it correctly.
	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.True(t, rootCA.CanSign())
	assert.NoError(t, err)

	// No private key here
	rootCA2, err := ca.GetLocalRootCA(paths.RootCA)
	assert.NoError(t, err)
	assert.Equal(t, rootCA.Cert, rootCA2.Cert)
	assert.False(t, rootCA2.CanSign())

	// write private key and assert we can load it and sign
	assert.NoError(t, ioutil.WriteFile(paths.RootCA.Key, rootCA.Signer.Key, os.FileMode(0600)))
	rootCA3, err := ca.GetLocalRootCA(paths.RootCA)
	assert.NoError(t, err)
	assert.Equal(t, rootCA.Cert, rootCA3.Cert)
	assert.True(t, rootCA3.CanSign())

	// Try with a private key that does not match the CA cert public key.
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), cryptorand.Reader)
	assert.NoError(t, err)
	privKeyBytes, err := x509.MarshalECPrivateKey(privKey)
	assert.NoError(t, err)
	privKeyPem := pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: privKeyBytes,
	})
	assert.NoError(t, ioutil.WriteFile(paths.RootCA.Key, privKeyPem, os.FileMode(0600)))

	_, err = ca.GetLocalRootCA(paths.RootCA)
	assert.EqualError(t, err, "certificate key mismatch")
}

func TestGetLocalRootCAInvalidCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	// Write some garbage to the CA cert
	require.NoError(t, ioutil.WriteFile(paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644))

	_, err = ca.GetLocalRootCA(paths.RootCA)
	require.Error(t, err)
}

func TestGetLocalRootCAInvalidKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)
	// Create the local Root CA to ensure that we can reload it correctly.
	_, err = ca.CreateRootCA("rootCN", paths.RootCA)
	require.NoError(t, err)

	// Write some garbage to the root key - this will cause the loading to fail
	require.NoError(t, ioutil.WriteFile(paths.RootCA.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0600))

	_, err = ca.GetLocalRootCA(paths.RootCA)
	require.Error(t, err)
}

func TestEncryptECPrivateKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	_, key, err := ca.GenerateNewCSR()
	assert.NoError(t, err)
	encryptedKey, err := ca.EncryptECPrivateKey(key, "passphrase")
	assert.NoError(t, err)

	keyBlock, _ := pem.Decode(encryptedKey)
	assert.NotNil(t, keyBlock)
	assert.Equal(t, keyBlock.Headers["Proc-Type"], "4,ENCRYPTED")
	assert.Contains(t, keyBlock.Headers["DEK-Info"], "AES-256-CBC")
}

func TestParseValidateAndSignCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	signedCert, err := rootCA.ParseValidateAndSignCSR(csr, "CN", "OU", "ORG")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	checkSingleCert(t, signedCert, "rootCN", "CN", "OU", "ORG")
}

func TestParseValidateAndSignMaliciousCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	req := &cfcsr.CertificateRequest{
		Names: []cfcsr.Name{
			{
				O:  "maliciousOrg",
				OU: "maliciousOU",
				L:  "maliciousLocality",
			},
		},
		CN:         "maliciousCN",
		Hosts:      []string{"docker.com"},
		KeyRequest: &cfcsr.BasicKeyRequest{A: "ecdsa", S: 256},
	}

	csr, _, err := cfcsr.ParseRequest(req)
	assert.NoError(t, err)

	signedCert, err := rootCA.ParseValidateAndSignCSR(csr, "CN", "OU", "ORG")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	checkSingleCert(t, signedCert, "rootCN", "CN", "OU", "ORG")
}

func TestGetRemoteCA(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	shaHash := sha256.New()
	shaHash.Write(tc.RootCA.Cert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	d, err := digest.Parse("sha256:" + mdStr)
	require.NoError(t, err)

	downloadedRootCA, err := ca.GetRemoteCA(tc.Context, d, tc.ConnBroker)
	require.NoError(t, err)
	require.Equal(t, downloadedRootCA.Cert, tc.RootCA.Cert)

	// update the test CA to include a multi-certificate bundle as the root - the digest
	// we use to verify with must be the digest of the whole bundle
	tmpDir, err := ioutil.TempDir("", "GetRemoteCA")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)
	paths := ca.NewConfigPaths(tmpDir)

	otherRootCA, err := ca.CreateRootCA("other", paths.RootCA)
	require.NoError(t, err)

	comboCertBundle := append(tc.RootCA.Cert, otherRootCA.Cert...)
	require.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, tc.Organization)
		cluster.RootCA.CACert = comboCertBundle
		cluster.RootCA.CAKey = tc.RootCA.Signer.Key
		return store.UpdateCluster(tx, cluster)
	}))
	require.NoError(t, raftutils.PollFunc(nil, func() error {
		_, err := ca.GetRemoteCA(tc.Context, d, tc.ConnBroker)
		if err == nil {
			return fmt.Errorf("testca's rootca hasn't updated yet")
		}
		require.Contains(t, err.Error(), "remote CA does not match fingerprint")
		return nil
	}))

	// If we provide the right digest, the root CA is updated and we can validate
	// certs signed by either one
	d = digest.FromBytes(comboCertBundle)
	downloadedRootCA, err = ca.GetRemoteCA(tc.Context, d, tc.ConnBroker)
	require.NoError(t, err)
	require.Equal(t, comboCertBundle, downloadedRootCA.Cert)
	require.Equal(t, 2, len(downloadedRootCA.Pool.Subjects()))

	for _, rootCA := range []ca.RootCA{tc.RootCA, otherRootCA} {
		krw := ca.NewKeyReadWriter(paths.Node, nil, nil)
		_, err := rootCA.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
		require.NoError(t, err)

		certPEM, _, err := krw.Read()
		require.NoError(t, err)

		cert, err := helpers.ParseCertificatesPEM(certPEM)
		require.NoError(t, err)

		chains, err := cert[0].Verify(x509.VerifyOptions{
			Roots: downloadedRootCA.Pool,
		})
		require.NoError(t, err)
		require.Len(t, chains, 1)
	}
}

func TestCanSign(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	assert.True(t, tc.RootCA.CanSign())
	tc.RootCA.Signer = nil
	assert.False(t, tc.RootCA.CanSign())
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	_, err := ca.GetRemoteCA(tc.Context, "sha256:2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b", tc.ConnBroker)
	assert.Error(t, err)
}

func TestRequestAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Copy the current RootCA without the signer
	rca := ca.RootCA{Cert: tc.RootCA.Cert, Pool: tc.RootCA.Pool}
	cert, err := rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter,
		ca.CertificateRequestConfig{
			Token:      tc.ManagerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())

	// there was no encryption config in the remote, so the key should be unencrypted
	unencryptedKeyReader := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	_, _, err = unencryptedKeyReader.Read()
	require.NoError(t, err)

	// the worker token is also unencrypted
	cert, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	_, _, err = unencryptedKeyReader.Read()
	require.NoError(t, err)

	// If there is a different kek in the remote store, when TLS certs are renewed the new key will
	// be encrypted with that kek
	assert.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, tc.Organization)
		cluster.Spec.EncryptionConfig.AutoLockManagers = true
		cluster.UnlockKeys = []*api.EncryptionKey{{
			Subsystem: ca.ManagerRole,
			Key:       []byte("kek!"),
		}}
		return store.UpdateCluster(tx, cluster)
	}))
	assert.NoError(t, os.RemoveAll(tc.Paths.Node.Cert))
	assert.NoError(t, os.RemoveAll(tc.Paths.Node.Key))

	_, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter,
		ca.CertificateRequestConfig{
			Token:      tc.ManagerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)

	// key can no longer be read without a kek
	_, _, err = unencryptedKeyReader.Read()
	require.Error(t, err)

	_, _, err = ca.NewKeyReadWriter(tc.Paths.Node, []byte("kek!"), nil).Read()
	require.NoError(t, err)

	// if it's a worker though, the key is always unencrypted, even though the manager key is encrypted
	_, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	_, _, err = unencryptedKeyReader.Read()
	require.NoError(t, err)
}

// TODO(cyli):  add test for RequestAndSaveNewCertificates but with intermediates - this involves adding
// support for appending intermediates on the CA server first

func TestIssueAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Test the creation of a manager certificate
	cert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN", ca.ManagerRole, tc.Organization)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())

	certBytes, err := ioutil.ReadFile(tc.Paths.Node.Cert)
	assert.NoError(t, err)

	checkSingleCert(t, certBytes, "swarm-test-CA", "CN", ca.ManagerRole, tc.Organization, ca.CARole)

	// Test the creation of a worker node cert
	cert, err = tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN", ca.WorkerRole, tc.Organization)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err = permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())

	certBytes, err = ioutil.ReadFile(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	checkSingleCert(t, certBytes, "swarm-test-CA", "CN", ca.WorkerRole, tc.Organization)
}

func TestGetRemoteSignedCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	certs, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.RootCA.Pool,
		ca.CertificateRequestConfig{
			Token:      tc.ManagerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	// Test the expiration for a manager certificate
	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 1)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.ManagerRole)

	// Test the expiration for an worker certificate
	certs, err = ca.GetRemoteSignedCertificate(tc.Context, csr, tc.RootCA.Pool,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 1)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.WorkerRole)
}

func TestGetRemoteSignedCertificateNodeInfo(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	cert, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.RootCA.Pool,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), state.EventCreateNode{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.RootCA.Pool,
			ca.CertificateRequestConfig{
				Token:      tc.WorkerToken,
				ConnBroker: tc.ConnBroker,
			})
		completed <- err
	}()

	event := <-updates
	node := event.(state.EventCreateNode).Node.Copy()

	// Directly update the status of the store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node.Certificate.Status.State = api.IssuanceStateIssued

		return store.UpdateNode(tx, node)
	})
	assert.NoError(t, err)

	// Make sure GetRemoteSignedCertificate didn't return an error
	assert.NoError(t, <-completed)
}

func TestNewRootCA(t *testing.T) {
	for _, pair := range []struct{ cert, key []byte }{
		{cert: testutils.ECDSA256SHA256Cert, key: testutils.ECDSA256Key},
		{cert: testutils.RSA2048SHA256Cert, key: testutils.RSA2048Key},
	} {
		rootCA, err := ca.NewRootCA(pair.cert, pair.key, ca.DefaultNodeCertExpiration)
		require.NoError(t, err, string(pair.key))
		require.Equal(t, pair.cert, rootCA.Cert)
		require.Equal(t, pair.key, rootCA.Signer.Key)
		// require.Equal(t, bytes.TrimSpace(pair.cert), bytes.TrimSpace(rootCA.Cert))
		// require.Equal(t, bytes.TrimSpace(pair.key), bytes.TrimSpace(rootCA.Signer.Key))
		require.NotNil(t, rootCA.Signer)
		_, err = rootCA.Digest.Verifier().Write(pair.cert)
		require.NoError(t, err)
	}
}

func TestNewRootCABundle(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	// make one rootCA
	secondRootCA, err := ca.CreateRootCA("rootCN2", paths.RootCA)
	assert.NoError(t, err)

	// make a second root CA
	firstRootCA, err := ca.CreateRootCA("rootCN1", paths.RootCA)
	assert.NoError(t, err)

	// Overwrite the bytes of the second Root CA with the bundle, creating a valid 2 cert bundle
	bundle := append(firstRootCA.Cert, secondRootCA.Cert...)
	err = ioutil.WriteFile(paths.RootCA.Cert, bundle, 0644)
	assert.NoError(t, err)

	newRootCA, err := ca.NewRootCA(bundle, firstRootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, bundle, newRootCA.Cert)
	assert.Equal(t, 2, len(newRootCA.Pool.Subjects()))

	// If I use newRootCA's IssueAndSaveNewCertificates to sign certs, I'll get the correct CA in the chain
	kw := ca.NewKeyReadWriter(paths.Node, nil, nil)
	_, err = newRootCA.IssueAndSaveNewCertificates(kw, "CN", "OU", "ORG")
	assert.NoError(t, err)

	certBytes, err := ioutil.ReadFile(paths.Node.Cert)
	assert.NoError(t, err)
	checkSingleCert(t, certBytes, "rootCN1", "CN", "OU", "ORG")
}

func TestNewRootCANonDefaultExpiry(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Signer.Key, 1*time.Hour)
	assert.NoError(t, err)

	// Create and sign a new CSR
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)
	cert, err := newRootCA.ParseValidateAndSignCSR(csr, "CN", ca.ManagerRole, "ORG")
	assert.NoError(t, err)

	parsedCerts, err := helpers.ParseCertificatesPEM(cert)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 1)
	assert.True(t, time.Now().Add(time.Minute*59).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(time.Hour).Add(time.Minute).After(parsedCerts[0].NotAfter))

	// Sign the same CSR again, this time with a 59 Minute expiration RootCA (under the 60 minute minimum).
	// This should use the default of 3 months
	newRootCA, err = ca.NewRootCA(rootCA.Cert, rootCA.Signer.Key, 59*time.Minute)
	assert.NoError(t, err)

	cert, err = newRootCA.ParseValidateAndSignCSR(csr, "CN", ca.ManagerRole, "ORG")
	assert.NoError(t, err)

	parsedCerts, err = helpers.ParseCertificatesPEM(cert)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 1)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
}

type invalidCertKeyTestCase struct {
	cert     []byte
	key      []byte
	errorStr string
}

func TestNewRootCAInvalidCertAndKeys(t *testing.T) {
	invalids := []invalidCertKeyTestCase{
		{
			cert:     []byte("malformed"),
			key:      testutils.ECDSA256Key,
			errorStr: "Failed to decode certificate",
		},
		{
			cert:     testutils.NotYetValidCert,
			key:      testutils.NotYetValidKey,
			errorStr: "not yet valid",
		},
		{
			cert:     testutils.ExpiredCert,
			key:      testutils.ExpiredKey,
			errorStr: "expired",
		},
		{
			cert:     testutils.RSA2048SHA1Cert,
			key:      testutils.RSA2048Key,
			errorStr: "unsupported signature algorithm",
		},
		{
			cert:     testutils.ECDSA256SHA1Cert,
			key:      testutils.ECDSA256Key,
			errorStr: "unsupported signature algorithm",
		},
		{
			cert:     testutils.ECDSA256SHA256Cert,
			key:      []byte("malformed"),
			errorStr: "malformed private key",
		},
		{
			cert:     testutils.RSA1024Cert,
			key:      testutils.RSA1024Key,
			errorStr: "unsupported RSA key parameters",
		},
		{
			cert:     testutils.ECDSA224Cert,
			key:      testutils.ECDSA224Key,
			errorStr: "unsupported ECDSA key parameters",
		},
		{
			cert:     testutils.ECDSA256SHA256Cert,
			key:      testutils.ECDSA224Key,
			errorStr: "certificate key mismatch",
		},
		{
			cert:     testutils.DSA2048Cert,
			key:      testutils.DSA2048Key,
			errorStr: "unsupported signature algorithm",
		},
	}

	for _, invalid := range invalids {
		_, err := ca.NewRootCA(invalid.cert, invalid.key, ca.DefaultNodeCertExpiration)
		require.Error(t, err, invalid.errorStr)
		require.Contains(t, err.Error(), invalid.errorStr)
	}
}

func TestNewRootCAWithPassphrase(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)
	defer os.Setenv(ca.PassphraseENVVar, "")
	defer os.Setenv(ca.PassphraseENVVarPrev, "")

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	// Ensure that we're encrypting the Key bytes out of NewRoot if there
	// is a passphrase set as an env Var
	os.Setenv(ca.PassphraseENVVar, "password1")
	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.NotEqual(t, rootCA.Signer.Key, newRootCA.Signer.Key)
	assert.Equal(t, rootCA.Cert, newRootCA.Cert)
	assert.NotContains(t, string(rootCA.Signer.Key), string(newRootCA.Signer.Key))
	assert.Contains(t, string(newRootCA.Signer.Key), "Proc-Type: 4,ENCRYPTED")

	// Ensure that we're decrypting the Key bytes out of NewRoot if there
	// is a passphrase set as an env Var
	anotherNewRootCA, err := ca.NewRootCA(newRootCA.Cert, newRootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, newRootCA, anotherNewRootCA)
	assert.NotContains(t, string(rootCA.Signer.Key), string(anotherNewRootCA.Signer.Key))
	assert.Contains(t, string(anotherNewRootCA.Signer.Key), "Proc-Type: 4,ENCRYPTED")

	// Ensure that we cant decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var
	os.Setenv(ca.PassphraseENVVar, "password2")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.Error(t, err)

	// Ensure that we cant decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var
	os.Setenv(ca.PassphraseENVVarPrev, "password2")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.Error(t, err)

	// Ensure that we can decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var, but a valid as Prev
	os.Setenv(ca.PassphraseENVVarPrev, "password1")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, newRootCA, anotherNewRootCA)
	assert.NotContains(t, string(rootCA.Signer.Key), string(anotherNewRootCA.Signer.Key))
	assert.Contains(t, string(anotherNewRootCA.Signer.Key), "Proc-Type: 4,ENCRYPTED")
}

type certTestCase struct {
	cert        []byte
	errorStr    string
	root        []byte
	allowExpiry bool
}

func TestValidateCertificateChain(t *testing.T) {
	leaf, intermediate, root := testutils.ECDSACertChain[0], testutils.ECDSACertChain[1], testutils.ECDSACertChain[2]
	intermediateKey, rootKey := testutils.ECDSACertChainKeys[1], testutils.ECDSACertChainKeys[2] // we don't care about the leaf key

	chain := func(certs ...[]byte) []byte {
		var all []byte
		for _, cert := range certs {
			all = append(all, cert...)
		}
		return all
	}

	now := time.Now()
	expiredLeaf := testutils.ReDateCert(t, leaf, intermediate, intermediateKey, now.Add(-10*time.Hour), now.Add(-1*time.Minute))
	expiredIntermediate := testutils.ReDateCert(t, intermediate, root, rootKey, now.Add(-10*time.Hour), now.Add(-1*time.Minute))
	notYetValidLeaf := testutils.ReDateCert(t, leaf, intermediate, intermediateKey, now.Add(time.Hour), now.Add(2*time.Hour))
	notYetValidIntermediate := testutils.ReDateCert(t, intermediate, root, rootKey, now.Add(time.Hour), now.Add(2*time.Hour))

	rootPool := x509.NewCertPool()
	rootPool.AppendCertsFromPEM(root)

	invalids := []certTestCase{
		{
			cert:     nil,
			root:     root,
			errorStr: "no certificates to validate",
		},
		{
			cert:     []byte("malformed"),
			root:     root,
			errorStr: "Failed to decode certificate",
		},
		{
			cert:     chain(leaf, intermediate, leaf),
			root:     root,
			errorStr: "certificates do not form a chain",
		},
		{
			cert:     chain(leaf, intermediate),
			root:     testutils.ECDSA256SHA256Cert,
			errorStr: "unknown authority",
		},
		{
			cert:     chain(expiredLeaf, intermediate),
			root:     root,
			errorStr: "not valid after",
		},
		{
			cert:     chain(leaf, expiredIntermediate),
			root:     root,
			errorStr: "not valid after",
		},
		{
			cert:     chain(notYetValidLeaf, intermediate),
			root:     root,
			errorStr: "not valid before",
		},
		{
			cert:     chain(leaf, notYetValidIntermediate),
			root:     root,
			errorStr: "not valid before",
		},

		// if we allow expiry, we still don't allow not yet valid certs or expired certs that don't chain up to the root
		{
			cert:        chain(notYetValidLeaf, intermediate),
			root:        root,
			allowExpiry: true,
			errorStr:    "not valid before",
		},
		{
			cert:        chain(leaf, notYetValidIntermediate),
			root:        root,
			allowExpiry: true,
			errorStr:    "not valid before",
		},
		{
			cert:        chain(expiredLeaf, intermediate),
			root:        testutils.ECDSA256SHA256Cert,
			allowExpiry: true,
			errorStr:    "unknown authority",
		},

		// construct a weird cases where one cert is expired, we allow expiry, but the other cert is not yet valid at the first cert's expiry
		// (this is not something that can happen unless we allow expiry, because if the cert periods don't overlap, one or the other will
		// be either not yet valid or already expired)
		{
			cert: chain(
				testutils.ReDateCert(t, leaf, intermediate, intermediateKey, now.Add(-3*helpers.OneDay), now.Add(-2*helpers.OneDay)),
				testutils.ReDateCert(t, intermediate, root, rootKey, now.Add(-1*helpers.OneDay), now.Add(helpers.OneDay))),
			root:        root,
			allowExpiry: true,
			errorStr:    "there is no time span",
		},
		// similarly, but for root pool
		{
			cert:        chain(expiredLeaf, expiredIntermediate),
			root:        testutils.ReDateCert(t, root, root, rootKey, now.Add(-3*helpers.OneYear), now.Add(-2*helpers.OneYear)),
			allowExpiry: true,
			errorStr:    "there is no time span",
		},
	}

	for _, invalid := range invalids {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(invalid.root)
		_, err := ca.ValidateCertChain(pool, invalid.cert, invalid.allowExpiry)
		require.Error(t, err, invalid.errorStr)
		require.Contains(t, err.Error(), invalid.errorStr)
	}

	// these will default to using the root pool, so we don't have to specify the root pool
	valids := []certTestCase{
		{cert: chain(leaf, intermediate, root)},
		{cert: chain(leaf, intermediate)},
		{cert: intermediate},
		{
			cert:        chain(expiredLeaf, intermediate),
			allowExpiry: true,
		},
		{
			cert:        chain(leaf, expiredIntermediate),
			allowExpiry: true,
		},
		{
			cert:        chain(expiredLeaf, expiredIntermediate),
			allowExpiry: true,
		},
	}

	for _, valid := range valids {
		_, err := ca.ValidateCertChain(rootPool, valid.cert, valid.allowExpiry)
		require.NoError(t, err)
	}
}

// Tests cross-signing using a certificate
func TestRootCACrossSignCACertificate(t *testing.T) {
	t.Parallel()

	cert1, key1, err := testutils.CreateRootCertAndKey("rootCN")
	require.NoError(t, err)

	rootCA1, err := ca.NewRootCA(cert1, key1, ca.DefaultNodeCertExpiration)
	require.NoError(t, err)

	cert2, key2, err := testutils.CreateRootCertAndKey("rootCN2")
	require.NoError(t, err)

	rootCA2, err := ca.NewRootCA(cert2, key2, ca.DefaultNodeCertExpiration)
	require.NoError(t, err)

	tempdir, err := ioutil.TempDir("", "cross-sign-cert")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	paths := ca.NewConfigPaths(tempdir)
	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)

	_, err = rootCA2.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)
	certBytes, _, err := krw.Read()
	require.NoError(t, err)
	leafCert, err := helpers.ParseCertificatePEM(certBytes)
	require.NoError(t, err)

	// cross-signing a non-CA fails
	_, err = rootCA1.CrossSignCACertificate(certBytes)
	require.Error(t, err)

	// cross-signing some non-cert PEM bytes fail
	_, err = rootCA1.CrossSignCACertificate(key1)
	require.Error(t, err)

	intermediate, err := rootCA1.CrossSignCACertificate(cert2)
	require.NoError(t, err)
	parsedIntermediate, err := helpers.ParseCertificatePEM(intermediate)
	require.NoError(t, err)
	parsedRoot2, err := helpers.ParseCertificatePEM(cert2)
	require.NoError(t, err)
	require.Equal(t, parsedRoot2.RawSubject, parsedIntermediate.RawSubject)
	require.Equal(t, parsedRoot2.RawSubjectPublicKeyInfo, parsedIntermediate.RawSubjectPublicKeyInfo)
	require.True(t, parsedIntermediate.IsCA)

	intermediatePool := x509.NewCertPool()
	intermediatePool.AddCert(parsedIntermediate)

	// we can validate a chain from the leaf to the first root through the intermediate,
	// or from the leaf cert to the second root with or without the intermediate
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA1.Pool})
	require.Error(t, err)
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA1.Pool, Intermediates: intermediatePool})
	require.NoError(t, err)

	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool})
	require.NoError(t, err)
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool, Intermediates: intermediatePool})
	require.NoError(t, err)
}
