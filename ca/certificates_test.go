package ca_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"io/ioutil"
	"os"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/distribution/digest"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func init() {
	os.Setenv(ca.PassphraseENVVar, "")
	os.Setenv(ca.PassphraseENVVarPrev, "")
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

func TestCreateAndWriteRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	_, err = ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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
	_, err = ca.GetLocalRootCA(tempBaseDir)
	assert.Equal(t, ca.ErrNoLocalRootCA, err)

	// Create the local Root CA to ensure that we can reload it correctly.
	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.True(t, rootCA.CanSign())
	assert.NoError(t, err)

	rootCA2, err := ca.GetLocalRootCA(tempBaseDir)
	assert.NoError(t, err)
	assert.True(t, rootCA2.CanSign())
	assert.Equal(t, rootCA, rootCA2)

	// Try again, this time without a private key.
	assert.NoError(t, os.Remove(paths.RootCA.Key))

	rootCA3, err := ca.GetLocalRootCA(tempBaseDir)
	assert.NoError(t, err)
	assert.False(t, rootCA3.CanSign())
	assert.Equal(t, rootCA.Cert, rootCA3.Cert)

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

	_, err = ca.GetLocalRootCA(tempBaseDir)
	assert.EqualError(t, err, "certificate key mismatch")
}

func TestGenerateAndSignNewTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	_, err = ca.GenerateAndSignNewTLSCert(rootCA, "CN", "OU", "ORG", paths.Node)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Node.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())

	certBytes, err := ioutil.ReadFile(paths.Node.Cert)
	assert.NoError(t, err)
	certs, err := helpers.ParseCertificatesPEM(certBytes)
	assert.NoError(t, err)
	assert.Len(t, certs, 2)
	assert.Equal(t, "CN", certs[0].Subject.CommonName)
	assert.Equal(t, "OU", certs[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, "ORG", certs[0].Subject.Organization[0])
	assert.Equal(t, "rootCN", certs[1].Subject.CommonName)
}

func TestGenerateAndWriteNewKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	csr, key, err := ca.GenerateAndWriteNewKey(paths.Node)
	assert.NoError(t, err)
	assert.NotNil(t, csr)
	assert.NotNil(t, key)

	perms, err := permbits.Stat(paths.Node.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())

	_, err = helpers.ParseCSRPEM(csr)
	assert.NoError(t, err)
}

func TestEncryptECPrivateKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	_, key, err := ca.GenerateAndWriteNewKey(paths.Node)
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

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	csr, _, err := ca.GenerateAndWriteNewKey(paths.Node)
	assert.NoError(t, err)

	signedCert, err := rootCA.ParseValidateAndSignCSR(csr, "CN", "OU", "ORG")
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 3, len(parsedCert[0].Subject.Names))
	assert.Equal(t, "ORG", parsedCert[0].Subject.Organization[0])
	assert.Equal(t, "rootCN", parsedCert[1].Subject.CommonName)
}

func TestParseValidateAndSignMaliciousCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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

	parsedCert, err := helpers.ParseCertificatesPEM(signedCert)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(parsedCert))
	assert.Equal(t, "CN", parsedCert[0].Subject.CommonName)
	assert.Equal(t, 1, len(parsedCert[0].Subject.OrganizationalUnit))
	assert.Equal(t, "OU", parsedCert[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, 3, len(parsedCert[0].Subject.Names))
	assert.Empty(t, parsedCert[0].Subject.Locality)
	assert.Equal(t, "ORG", parsedCert[0].Subject.Organization[0])
	assert.Equal(t, "rootCN", parsedCert[1].Subject.CommonName)
}

func TestGetRemoteCA(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	shaHash := sha256.New()
	shaHash.Write(tc.RootCA.Cert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	d, err := digest.ParseDigest("sha256:" + mdStr)
	assert.NoError(t, err)

	cert, err := ca.GetRemoteCA(tc.Context, d, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
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

	_, err := ca.GetRemoteCA(tc.Context, "sha256:2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b", tc.Picker)
	assert.Error(t, err)
}

func TestRequestAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	info := make(chan api.IssueNodeCertificateResponse, 1)
	// Copy the current RootCA without the signer
	rca := ca.RootCA{Cert: tc.RootCA.Cert, Pool: tc.RootCA.Pool}
	cert, err := rca.RequestAndSaveNewCertificates(tc.Context, tc.Paths.Node, tc.WorkerToken, tc.Picker, nil, info)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	assert.NotEmpty(t, <-info)
}

func TestIssueAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Copy the current RootCA without the signer
	cert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.Paths.Node, "CN", ca.ManagerRole, tc.Organization)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())

	certBytes, err := ioutil.ReadFile(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	certs, err := helpers.ParseCertificatesPEM(certBytes)
	assert.NoError(t, err)
	assert.Len(t, certs, 2)
	assert.Equal(t, "CN", certs[0].Subject.CommonName)
	assert.Equal(t, ca.ManagerRole, certs[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, tc.Organization, certs[0].Subject.Organization[0])
	assert.Equal(t, "swarm-test-CA", certs[1].Subject.CommonName)
}

func TestGetRemoteSignedCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	certs, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.ManagerToken, tc.RootCA.Pool, tc.Picker, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	// Test the expiration for a manager certificate
	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.ManagerRole)

	// Test the expiration for an agent certificate
	certs, err = ca.GetRemoteSignedCertificate(tc.Context, csr, tc.WorkerToken, tc.RootCA.Pool, tc.Picker, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.AgentRole)
}

func TestGetRemoteSignedCertificateNodeInfo(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	info := make(chan api.IssueNodeCertificateResponse, 1)
	cert, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, tc.Picker, nil, info)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	assert.NotEmpty(t, <-info)
}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), state.EventCreateNode{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, tc.Picker, nil, nil)
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

func TestBootstrapCluster(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	err = ca.BootstrapCluster(tempBaseDir)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.RootCA.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.RootCA.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())

	perms, err = permbits.Stat(paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Node.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestNewRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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

	// Write one root CA to disk, keep the bytes
	secondRootCA, err := ca.CreateAndWriteRootCA("rootCN2", paths.RootCA)
	assert.NoError(t, err)

	// Overwrite the first root CA on disk
	firstRootCA, err := ca.CreateAndWriteRootCA("rootCN1", paths.RootCA)
	assert.NoError(t, err)

	// Overwrite the bytes of the second Root CA with the bundle, creating a valid 2 cert bundle
	bundle := append(firstRootCA.Cert, secondRootCA.Cert...)
	err = ioutil.WriteFile(paths.RootCA.Cert, bundle, 0644)
	assert.NoError(t, err)

	newRootCA, err := ca.NewRootCA(bundle, firstRootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	assert.Equal(t, bundle, newRootCA.Cert)
	assert.Equal(t, 2, len(newRootCA.Pool.Subjects()))

	// Now load the bundle from disk
	diskRootCA, err := ca.GetLocalRootCA(tempBaseDir)
	assert.NoError(t, err)
	assert.Equal(t, bundle, diskRootCA.Cert)
	assert.Equal(t, 2, len(diskRootCA.Pool.Subjects()))

	// If I use GenerateAndSignNewTLSCert to sign certs, I'll get the correct CA in the chain
	_, err = ca.GenerateAndSignNewTLSCert(diskRootCA, "CN", "OU", "ORG", paths.Node)
	assert.NoError(t, err)

	certBytes, err := ioutil.ReadFile(paths.Node.Cert)
	assert.NoError(t, err)
	certs, err := helpers.ParseCertificatesPEM(certBytes)
	assert.NoError(t, err)
	assert.Len(t, certs, 2)
	assert.Equal(t, "CN", certs[0].Subject.CommonName)
	assert.Equal(t, "OU", certs[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, "ORG", certs[0].Subject.Organization[0])
	assert.Equal(t, "rootCN1", certs[1].Subject.CommonName)

}

func TestNewRootCANonDefaultExpiry(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	newRootCA, err := ca.NewRootCA(rootCA.Cert, rootCA.Key, 1*time.Hour)
	assert.NoError(t, err)

	// Create and sign a new CSR
	csr, _, err := ca.GenerateAndWriteNewKey(paths.Node)
	assert.NoError(t, err)
	cert, err := newRootCA.ParseValidateAndSignCSR(csr, "CN", ca.ManagerRole, "ORG")
	assert.NoError(t, err)

	parsedCerts, err := helpers.ParseCertificatesPEM(cert)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(time.Minute*50).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(time.Hour).After(parsedCerts[0].NotAfter))

	// Sign the same CSR again, this time with a 29 Minute expiration RootCA (under the 30 minute minimum).
	// This should use the default of 3 months
	newRootCA, err = ca.NewRootCA(rootCA.Cert, rootCA.Key, 29*time.Minute)
	assert.NoError(t, err)

	cert, err = newRootCA.ParseValidateAndSignCSR(csr, "CN", ca.ManagerRole, "ORG")
	assert.NoError(t, err)

	parsedCerts, err = helpers.ParseCertificatesPEM(cert)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
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

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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
