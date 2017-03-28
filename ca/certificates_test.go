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
	"net"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/distribution/digest"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/identity"
	"github.com/docker/swarmkit/manager/state"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/remotes"
	"github.com/phayes/permbits"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	cert, err := ca.GetRemoteCA(tc.Context, d, tc.Remotes)
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

	_, err := ca.GetRemoteCA(tc.Context, "sha256:2d2f968475269f0dde5299427cf74348ee1d6115b95c6e3f283e5a4de8da445b", tc.Remotes)
	assert.Error(t, err)
}

func TestRequestAndSaveNewCertificates(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	info := make(chan api.IssueNodeCertificateResponse, 1)
	// Copy the current RootCA without the signer
	rca := ca.RootCA{Cert: tc.RootCA.Cert, Pool: tc.RootCA.Pool}
	cert, err := rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter, tc.ManagerToken, tc.Remotes, nil, info)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	perms, err := permbits.Stat(tc.Paths.Node.Cert)
	assert.NoError(t, err)
	assert.False(t, perms.GroupWrite())
	assert.False(t, perms.OtherWrite())
	assert.NotEmpty(t, <-info)

	// there was no encryption config in the remote, so the key should be unencrypted
	unencryptedKeyReader := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	_, _, err = unencryptedKeyReader.Read()
	require.NoError(t, err)

	// the worker token is also unencrypted
	cert, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter, tc.WorkerToken, tc.Remotes, nil, info)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	assert.NotEmpty(t, <-info)
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

	_, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter, tc.ManagerToken, tc.Remotes, nil, info)
	assert.NoError(t, err)
	assert.NotEmpty(t, <-info)

	// key can no longer be read without a kek
	_, _, err = unencryptedKeyReader.Read()
	require.Error(t, err)

	_, _, err = ca.NewKeyReadWriter(tc.Paths.Node, []byte("kek!"), nil).Read()
	require.NoError(t, err)

	// if it's a worker though, the key is always unencrypted, even though the manager key is encrypted
	_, err = rca.RequestAndSaveNewCertificates(tc.Context, tc.KeyReadWriter, tc.WorkerToken, tc.Remotes, nil, info)
	assert.NoError(t, err)
	assert.NotEmpty(t, <-info)
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
	certs, err := helpers.ParseCertificatesPEM(certBytes)
	assert.NoError(t, err)
	assert.Len(t, certs, 2)
	assert.Equal(t, "CN", certs[0].Subject.CommonName)
	assert.Equal(t, ca.ManagerRole, certs[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, tc.Organization, certs[0].Subject.Organization[0])
	assert.Equal(t, "swarm-test-CA", certs[1].Subject.CommonName)
	assert.Contains(t, certs[0].DNSNames, "CN")
	assert.Contains(t, certs[0].DNSNames, "swarm-ca")
	assert.Contains(t, certs[0].DNSNames, "swarm-manager")

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
	certs, err = helpers.ParseCertificatesPEM(certBytes)
	assert.NoError(t, err)
	assert.Len(t, certs, 2)
	assert.Equal(t, "CN", certs[0].Subject.CommonName)
	assert.Equal(t, ca.WorkerRole, certs[0].Subject.OrganizationalUnit[0])
	assert.Equal(t, tc.Organization, certs[0].Subject.Organization[0])
	assert.Equal(t, "swarm-test-CA", certs[1].Subject.CommonName)
	assert.Contains(t, certs[0].DNSNames, "CN")
	assert.Contains(t, certs[0].DNSNames, "swarm-worker")

}

func TestGetRemoteSignedCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	certs, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.ManagerToken, tc.RootCA.Pool, tc.Remotes, nil, nil, 0)
	assert.NoError(t, err)
	assert.NotNil(t, certs)

	// Test the expiration for a manager certificate
	parsedCerts, err := helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, -1).Before(parsedCerts[0].NotAfter))
	assert.True(t, time.Now().Add(ca.DefaultNodeCertExpiration).AddDate(0, 0, 1).After(parsedCerts[0].NotAfter))
	assert.Equal(t, parsedCerts[0].Subject.OrganizationalUnit[0], ca.ManagerRole)

	// Test the expiration for an worker certificate
	certs, err = ca.GetRemoteSignedCertificate(tc.Context, csr, tc.WorkerToken, tc.RootCA.Pool, tc.Remotes, nil, nil, 0)
	assert.NoError(t, err)
	assert.NotNil(t, certs)
	parsedCerts, err = helpers.ParseCertificatesPEM(certs)
	assert.NoError(t, err)
	assert.Len(t, parsedCerts, 2)
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

	info := make(chan api.IssueNodeCertificateResponse, 1)
	cert, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, tc.Remotes, nil, info, 0)
	assert.NoError(t, err)
	assert.NotNil(t, cert)
	assert.NotEmpty(t, <-info)
}

// A CA Server implementation that doesn't actually sign anything - something else
// will have to update the memory store to have a valid value for a node
type nonSigningCAServer struct {
	tc               *testutils.TestCA
	server           *grpc.Server
	addr             string
	nodeStatusCalled int64
}

func newNonSigningCAServer(t *testing.T, tc *testutils.TestCA) *nonSigningCAServer {
	secConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	require.NoError(t, err)
	serverOpts := []grpc.ServerOption{grpc.Creds(secConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(serverOpts...)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	n := &nonSigningCAServer{
		tc:     tc,
		addr:   l.Addr().String(),
		server: grpcServer,
	}

	api.RegisterNodeCAServer(grpcServer, n)
	go grpcServer.Serve(l)
	return n
}

func (n *nonSigningCAServer) stop(t *testing.T) {
	n.server.Stop()
}

func (n *nonSigningCAServer) getRemotes() remotes.Remotes {
	return remotes.NewRemotes(api.Peer{Addr: n.addr})
}

// only returns the status in the store
func (n *nonSigningCAServer) NodeCertificateStatus(ctx context.Context, request *api.NodeCertificateStatusRequest) (*api.NodeCertificateStatusResponse, error) {
	atomic.AddInt64(&n.nodeStatusCalled, 1)
	for {
		var node *api.Node
		n.tc.MemoryStore.View(func(tx store.ReadTx) {
			node = store.GetNode(tx, request.NodeID)
		})
		if node != nil && node.Certificate.Status.State == api.IssuanceStateIssued {
			return &api.NodeCertificateStatusResponse{
				Status:      &node.Certificate.Status,
				Certificate: &node.Certificate,
			}, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (n *nonSigningCAServer) IssueNodeCertificate(ctx context.Context, request *api.IssueNodeCertificateRequest) (*api.IssueNodeCertificateResponse, error) {
	nodeID := identity.NewID()
	role := api.NodeRoleWorker
	if n.tc.ManagerToken == request.Token {
		role = api.NodeRoleManager
	}

	// Create a new node
	err := n.tc.MemoryStore.Update(func(tx store.Tx) error {
		node := &api.Node{
			ID: nodeID,
			Certificate: api.Certificate{
				CSR:  request.CSR,
				CN:   nodeID,
				Role: role,
				Status: api.IssuanceStatus{
					State: api.IssuanceStatePending,
				},
			},
			Spec: api.NodeSpec{
				Role:       role,
				Membership: api.NodeMembershipAccepted,
			},
		}

		return store.CreateNode(tx, node)
	})
	if err != nil {
		return nil, err
	}
	return &api.IssueNodeCertificateResponse{
		NodeID:         nodeID,
		NodeMembership: api.NodeMembershipAccepted,
	}, nil
}

func TestGetRemoteSignedCertificateWithPending(t *testing.T) {
	t.Parallel()
	if testutils.External {
		// we don't actually need an external signing server, since we're faking a CA which
		// doesn't really sign
		return
	}

	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	require.NoError(t, tc.CAServer.Stop())

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)

	updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), state.EventCreateNode{})
	defer cancel()

	fakeCAServer := newNonSigningCAServer(t, tc)

	completed := make(chan error)
	defer close(completed)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, fakeCAServer.getRemotes(), nil, nil, 500*time.Millisecond)
		completed <- err
	}()

	var node *api.Node
	// wait for a new node to show up
	for node == nil {
		select {
		case event := <-updates: // we want to skip the first node, which is the test CA
			n := event.(state.EventCreateNode).Node.Copy()
			if n.Certificate.Status.State == api.IssuanceStatePending {
				node = n
			}
		}
	}

	// wait for the calls to NodeCertificateStatus to begin on the first signing server before we start timing
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		if atomic.LoadInt64(&fakeCAServer.nodeStatusCalled) == 0 {
			return fmt.Errorf("waiting for NodeCertificateStatus to be called")
		}
		return nil
	}, time.Second*2))

	// wait for 2.5 seconds and ensure that GetRemoteSignedCertificate has not returned with an error yet -
	// the first attempt to get the certificate status should have timed out after 500 milliseconds, but
	// it should have tried to poll again.  Add a few seconds for fudge time to make sure it's actually
	// still polling.
	select {
	case <-completed:
		require.FailNow(t, "GetRemoteSignedCertificate should wait at least 500 milliseconds")
	case <-time.After(2500 * time.Millisecond):
		// good, it's still polling so we can proceed with the test
	}
	require.True(t, atomic.LoadInt64(&fakeCAServer.nodeStatusCalled) > 1, "expected NodeCertificateStatus to have been polled more than once")

	// Directly update the status of the store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node.Certificate.Status.State = api.IssuanceStateIssued
		return store.UpdateNode(tx, node)
	})
	require.NoError(t, err)

	// Make sure GetRemoteSignedCertificate didn't return an error
	require.NoError(t, <-completed)

	// make sure if we time out the GetRemoteSignedCertificate call, it cancels immediately and doesn't keep
	// polling the status
	go func() {
		ctx, _ := context.WithTimeout(context.Background(), 1*time.Second)

		_, err := ca.GetRemoteSignedCertificate(ctx, csr, tc.WorkerToken, tc.RootCA.Pool, fakeCAServer.getRemotes(), nil, nil, 0)
		completed <- err
	}()

	// wait for 3 seconds and ensure that GetRemoteSignedCertificate has returned with a context DeadlineExceeded
	// error - it should have returned after 1 second, but add some more for rudge time.
	select {
	case err = <-completed:
		require.Equal(t, grpc.Code(err), codes.DeadlineExceeded)
	case <-time.After(3 * time.Second):
		require.FailNow(t, "GetRemoteSignedCertificate should have been canceled after 1 second, and it has been 3")
	}
}

// fake remotes interface that just selects the remotes in order
type fakeRemotes struct {
	mu    sync.Mutex
	peers []api.Peer
}

func (f *fakeRemotes) Weights() map[api.Peer]int {
	panic("this is not called")
}

func (f *fakeRemotes) Select(...string) (api.Peer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.peers) > 0 {
		return f.peers[0], nil
	}
	return api.Peer{}, fmt.Errorf("no more peers")
}

func (f *fakeRemotes) Observe(peer api.Peer, weight int) {
	f.ObserveIfExists(peer, weight)
}

// just removes a peer if the weight is negative
func (f *fakeRemotes) ObserveIfExists(peer api.Peer, weight int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if weight < 0 {
		var newPeers []api.Peer
		for _, p := range f.peers {
			if p != peer {
				newPeers = append(newPeers, p)
			}
		}
		f.peers = newPeers
	}
}

func (f *fakeRemotes) Remove(addrs ...api.Peer) {
	panic("this is not called")
}

var _ remotes.Remotes = &fakeRemotes{}

// On connection errors, so long as they happen after IssueNodeCertificate is successful, GetRemoteSignedCertificate
// tries to open a new connection and continue polling for NodeCertificateStatus.  If there are no more connections,
// then fail.
func TestGetRemoteSignedCertificateConnectionErrors(t *testing.T) {
	t.Parallel()
	if testutils.External {
		// we don't actually need an external signing server, since we're faking a CA which
		// doesn't really sign
		return
	}

	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	require.NoError(t, tc.CAServer.Stop())

	// Create a new CSR to be signed
	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)

	// create 2 CA servers referencing the same memory store, so we can have multiple connections
	fakeSigningServers := []*nonSigningCAServer{newNonSigningCAServer(t, tc), newNonSigningCAServer(t, tc)}
	defer fakeSigningServers[0].stop(t)
	defer fakeSigningServers[1].stop(t)
	multiRemotes := &fakeRemotes{
		peers: []api.Peer{
			{Addr: fakeSigningServers[0].addr},
			{Addr: fakeSigningServers[1].addr},
		},
	}

	completed, done := make(chan error), make(chan struct{})
	defer close(completed)
	defer close(done)
	go func() {
		_, err := ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, multiRemotes, nil, nil, 0)
		select {
		case <-done:
		case completed <- err:
		}
	}()

	// wait for the calls to NodeCertificateStatus to begin on the first signing server
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		if atomic.LoadInt64(&fakeSigningServers[0].nodeStatusCalled) == 0 {
			return fmt.Errorf("waiting for NodeCertificateStatus to be called")
		}
		return nil
	}, time.Second*2))

	// stop 1 server, because it will have been the remote GetRemoteSignedCertificate first connected to, and ensure
	// that GetRemoteSignedCertificate is still going
	fakeSigningServers[0].stop(t)
	select {
	case <-completed:
		require.FailNow(t, "GetRemoteSignedCertificate should still be going after 2.5 seconds")
	case <-time.After(2500 * time.Millisecond):
		// good, it's still polling so we can proceed with the test
	}

	// wait for the calls to NodeCertificateStatus to begin on the second signing server
	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		if atomic.LoadInt64(&fakeSigningServers[1].nodeStatusCalled) == 0 {
			return fmt.Errorf("waiting for NodeCertificateStatus to be called")
		}
		return nil
	}, time.Second*2))

	// kill the last server - this should cause GetRemoteSignedCertificate to fail because there are no more peers
	fakeSigningServers[1].stop(t)
	// wait for 5 seconds and ensure that GetRemoteSignedCertificate has returned with an error.
	select {
	case err = <-completed:
		require.Contains(t, err.Error(), "no more peers")
	case <-time.After(5 * time.Second):
		require.FailNow(t, "GetRemoteSignedCertificate should errored after 5 seconds")
	}

	// calling GetRemoteSignedCertificate with a connection that doesn't work with IssueNodeCertificate will fail
	// immediately without retrying with a new connection
	fakeSigningServers[1] = newNonSigningCAServer(t, tc)
	defer fakeSigningServers[1].stop(t)
	multiRemotes = &fakeRemotes{
		peers: []api.Peer{
			{Addr: fakeSigningServers[0].addr},
			{Addr: fakeSigningServers[1].addr},
		},
	}
	_, err = ca.GetRemoteSignedCertificate(context.Background(), csr, tc.WorkerToken, tc.RootCA.Pool, multiRemotes, nil, nil, 0)
	require.Error(t, err)
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
	assert.Len(t, parsedCerts, 2)
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
