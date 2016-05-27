package ca_test

import (
	"crypto/sha256"
	"encoding/hex"
	"io/ioutil"
	"os"
	"testing"
	"time"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

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
	assert.True(t, time.Now().Add(time.Hour*24*364*20).Before(parsedCert.NotAfter))

}

func TestGetLocalRootCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	rootCA2, err := ca.GetLocalRootCA(tempBaseDir)
	assert.NoError(t, err)
	assert.Equal(t, rootCA, rootCA2)
}

func TestGenerateAndSignNewTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	_, err = ca.GenerateAndSignNewTLSCert(rootCA, "CN", "OU", paths.Node)
	assert.NoError(t, err)

	perms, err := permbits.Stat(paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Node.Key)
	assert.NoError(t, err)
	assert.False(t, perms.GroupRead())
	assert.False(t, perms.OtherRead())
}

func TestGenerateAndWriteNewCSR(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := ca.NewConfigPaths(tempBaseDir)

	csr, key, err := ca.GenerateAndWriteNewCSR(paths.Node)
	assert.NoError(t, err)
	assert.NotNil(t, csr)
	assert.NotNil(t, key)

	perms, err := permbits.Stat(paths.Node.CSR)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	perms, err = permbits.Stat(paths.Node.Key)
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

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
	assert.NoError(t, err)

	csr, _, err := ca.GenerateAndWriteNewCSR(paths.Node)
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

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("rootCN", paths.RootCA)
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
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	shaHash := sha256.New()
	shaHash.Write(tc.RootCA.Cert)
	md := shaHash.Sum(nil)
	mdStr := hex.EncodeToString(md)

	cert, err := ca.GetRemoteCA(tc.Context, mdStr, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
}

func TestCanSign(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	assert.True(t, tc.RootCA.CanSign())
	tc.RootCA.Signer = nil
	assert.False(t, tc.RootCA.CanSign())
}

func TestGetRemoteCAInvalidHash(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	_, err := ca.GetRemoteCA(tc.Context, "2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b", tc.Picker)
	assert.Error(t, err)
}

func TestRequestAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Copy the current RootCA without the signer
	rca := ca.RootCA{Cert: tc.RootCA.Cert, Pool: tc.RootCA.Pool}
	cert, err := rca.RequestAndSaveNewCertificates(tc.Context, tc.Paths.Node, ca.ManagerRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
}

func TestIssueAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Copy the current RootCA without the signer
	cert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.Paths.Node, "CN", ca.ManagerRole)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
}

func TestGetRemoteSignedCertificateAutoAccept(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	certs, err := ca.GetRemoteSignedCertificate(context.Background(), csr, ca.ManagerRole, tc.RootCA.Pool, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	// TODO(diogo): change this back to three months
	// assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(time.Minute*50).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.ManagerRole)

	certs, err = ca.GetRemoteSignedCertificate(tc.Context, csr, ca.AgentRole, tc.RootCA.Pool, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	// TODO(diogo): change this back to three months
	// assert.True(t, time.Now().Add(time.Hour*24*29*3).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(time.Minute*50).Before(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.AgentRole)

}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), state.EventCreateNode{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, ca.ManagerRole, tc.RootCA.Pool, tc.Picker, nil)
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

func TestGetRemoteSignedCertificateRejected(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), state.EventCreateNode{})
	defer cancel()

	completed := make(chan error)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, ca.ManagerRole, tc.RootCA.Pool, tc.Picker, nil)
		completed <- err
	}()

	event := <-updates
	node := event.(state.EventCreateNode).Node.Copy()

	// Directly update the status of the store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node.Certificate.Status.State = api.IssuanceStateRejected

		return store.UpdateNode(tx, node)
	})
	assert.NoError(t, err)

	// Make sure GetRemoteSignedCertificate didn't return an error
	assert.EqualError(t, <-completed, "certificate issuance rejected: ISSUANCE_REJECTED")
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
