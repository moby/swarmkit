package ca_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
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
	assert.NoError(t, ioutil.WriteFile(paths.RootCA.Key, rootCA.Key, os.FileMode(0600)))
	rootCA3, err := ca.GetLocalRootCA(paths.RootCA)
	assert.NoError(t, err)
	assert.Equal(t, rootCA.Cert, rootCA3.Cert)
	assert.True(t, rootCA3.CanSign())

	// Try with a private key that does not match the CA cert public key.
	privKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
		cluster.RootCA.CAKey = tc.RootCA.Key
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
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, rootCA, newRootCA)
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

	newRootCA, err := ca.NewRootCA(bundle, firstRootCA.Key, ca.DefaultNodeCertExpiration)
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

	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Key, 1*time.Hour)
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
	newRootCA, err = ca.NewRootCA(rootCA.Cert, rootCA.Key, 59*time.Minute)
	assert.NoError(t, err)

	cert, err = newRootCA.ParseValidateAndSignCSR(csr, "CN", ca.ManagerRole, "ORG")
	assert.NoError(t, err)

	parsedCerts, err = helpers.ParseCertificatesPEM(cert)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 1)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
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
	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.NotEqual(t, rootCA.Key, newRootCA.Key)
	assert.Equal(t, rootCA.Cert, newRootCA.Cert)
	assert.NotContains(t, string(rootCA.Key), string(newRootCA.Key))
	assert.Contains(t, string(newRootCA.Key), "Proc-Type: 4,ENCRYPTED")

	// Ensure that we're decrypting the Key bytes out of NewRoot if there
	// is a passphrase set as an env Var
	anotherNewRootCA, err := ca.NewRootCA(newRootCA.Cert, newRootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, newRootCA, anotherNewRootCA)
	assert.NotContains(t, string(rootCA.Key), string(anotherNewRootCA.Key))
	assert.Contains(t, string(anotherNewRootCA.Key), "Proc-Type: 4,ENCRYPTED")

	// Ensure that we cant decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var
	os.Setenv(ca.PassphraseENVVar, "password2")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Key, ca.DefaultNodeCertExpiration)
	assert.Error(t, err)

	// Ensure that we cant decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var
	os.Setenv(ca.PassphraseENVVarPrev, "password2")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Key, ca.DefaultNodeCertExpiration)
	assert.Error(t, err)

	// Ensure that we can decrypt the Key bytes out of NewRoot if there
	// is a wrong passphrase set as an env Var, but a valid as Prev
	os.Setenv(ca.PassphraseENVVarPrev, "password1")
	anotherNewRootCA, err = ca.NewRootCA(newRootCA.Cert, newRootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, newRootCA, anotherNewRootCA)
	assert.NotContains(t, string(rootCA.Key), string(anotherNewRootCA.Key))
	assert.Contains(t, string(anotherNewRootCA.Key), "Proc-Type: 4,ENCRYPTED")

}
