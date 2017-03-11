package ca_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/ioutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDownloadRootCASuccess(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove the CA cert
	os.RemoveAll(tc.Paths.RootCA.Cert)

	rootCA, err := ca.DownloadRootCA(tc.Context, tc.Paths.RootCA, tc.WorkerToken, tc.ConnBroker)
	require.NoError(t, err)
	require.NotNil(t, rootCA.Pool)
	require.NotNil(t, rootCA.Cert)
	require.Nil(t, rootCA.Signer)
	require.False(t, rootCA.CanSign())
	require.Equal(t, tc.RootCA.Cert, rootCA.Cert)

	// Remove the CA cert
	os.RemoveAll(tc.Paths.RootCA.Cert)

	// downloading without a join token also succeeds
	rootCA, err = ca.DownloadRootCA(tc.Context, tc.Paths.RootCA, "", tc.ConnBroker)
	require.NoError(t, err)
	require.NotNil(t, rootCA.Pool)
	require.NotNil(t, rootCA.Cert)
	require.Nil(t, rootCA.Signer)
	require.False(t, rootCA.CanSign())
	require.Equal(t, tc.RootCA.Cert, rootCA.Cert)
}

func TestDownloadRootCAWrongCAHash(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove the CA cert
	os.RemoveAll(tc.Paths.RootCA.Cert)

	// invalid token
	for _, invalid := range []string{
		"invalidtoken", // completely invalid
		"SWMTKN-1-3wkodtpeoipd1u1hi0ykdcdwhw16dk73ulqqtn14b3indz68rf-4myj5xihyto11dg1cn55w8p6", // mistyped
	} {
		_, err := ca.DownloadRootCA(tc.Context, tc.Paths.RootCA, invalid, tc.ConnBroker)
		require.Error(t, err)
		require.Contains(t, err.Error(), "invalid join token")
	}

	// invalid hash token
	splitToken := strings.Split(tc.ManagerToken, "-")
	splitToken[2] = "1kxftv4ofnc6mt30lmgipg6ngf9luhwqopfk1tz6bdmnkubg0e"
	replacementToken := strings.Join(splitToken, "-")

	os.RemoveAll(tc.Paths.RootCA.Cert)

	_, err := ca.DownloadRootCA(tc.Context, tc.Paths.RootCA, replacementToken, tc.ConnBroker)
	require.Error(t, err)
	require.Contains(t, err.Error(), "remote CA does not match fingerprint.")
}

func TestCreateSecurityConfigEmptyDir(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new node
	os.RemoveAll(tc.TempDir)
	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	nodeConfig, err := tc.RootCA.CreateSecurityConfig(tc.Context, krw,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.Equal(t, tc.RootCA, *nodeConfig.RootCA())
}

func TestCreateSecurityConfigNoCerts(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are locally signed
	os.RemoveAll(tc.Paths.Node.Cert)
	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	nodeConfig, err := tc.RootCA.CreateSecurityConfig(tc.Context, krw,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.Equal(t, tc.RootCA, *nodeConfig.RootCA())

	// Remove only the node certificates form the directory, get a new rootCA, and attest that we get
	// new certificates that are issued by the remote CA
	os.RemoveAll(tc.Paths.Node.Cert)
	rootCA, err := ca.GetLocalRootCA(tc.Paths.RootCA)
	assert.NoError(t, err)
	nodeConfig, err = rootCA.CreateSecurityConfig(tc.Context, krw,
		ca.CertificateRequestConfig{
			Token:      tc.WorkerToken,
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.Equal(t, rootCA, *nodeConfig.RootCA())
}

func TestLoadSecurityConfigExpiredCert(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	now := time.Now()

	_, err := tc.RootCA.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)
	certBytes, _, err := krw.Read()
	require.NoError(t, err)

	// A cert that is not yet valid is not valid even if expiry is allowed
	invalidCert := testutils.ReDateCert(t, certBytes, tc.RootCA.Cert, tc.RootCA.Signer.Key, now.Add(time.Hour), now.Add(time.Hour*2))
	require.NoError(t, ioutil.WriteFile(tc.Paths.Node.Cert, invalidCert, 0700))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, true)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	// a cert that is expired is not valid if expiry is not allowed
	invalidCert = testutils.ReDateCert(t, certBytes, tc.RootCA.Cert, tc.RootCA.Signer.Key, now.Add(-2*time.Minute), now.Add(-1*time.Minute))
	require.NoError(t, ioutil.WriteFile(tc.Paths.Node.Cert, invalidCert, 0700))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	// but it is valid if expiry is allowed
	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, true)
	require.NoError(t, err)
}

func TestLoadSecurityConfigInvalidCert(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.Node.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)

	_, err := ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	assert.Error(t, err)

	nodeConfig, err := tc.RootCA.CreateSecurityConfig(tc.Context, krw,
		ca.CertificateRequestConfig{
			ConnBroker: tc.ConnBroker,
		})

	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.Equal(t, tc.RootCA, *nodeConfig.RootCA())
}

func TestLoadSecurityConfigInvalidKey(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)

	_, err := ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	assert.Error(t, err)

	nodeConfig, err := tc.RootCA.CreateSecurityConfig(tc.Context, krw,
		ca.CertificateRequestConfig{
			ConnBroker: tc.ConnBroker,
		})
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.Equal(t, tc.RootCA, *nodeConfig.RootCA())
}

func TestLoadSecurityConfigIncorrectPassphrase(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	paths := ca.NewConfigPaths(tc.TempDir)
	_, err := tc.RootCA.IssueAndSaveNewCertificates(ca.NewKeyReadWriter(paths.Node, []byte("kek"), nil),
		"nodeID", ca.WorkerRole, tc.Organization)
	require.NoError(t, err)

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, ca.NewKeyReadWriter(paths.Node, nil, nil), false)
	require.IsType(t, ca.ErrInvalidKEK{}, err)
}

func TestLoadSecurityConfigIntermediates(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-load-config-with-intermediates")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	paths := ca.NewConfigPaths(tempdir)
	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)

	rootCA, err := ca.NewRootCA(testutils.ECDSACertChain[2], nil, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	// loading the incomplete chain fails
	require.NoError(t, krw.Write(testutils.ECDSACertChain[0], testutils.ECDSACertChainKeys[0], nil))
	_, err = ca.LoadSecurityConfig(context.Background(), rootCA, krw, false)
	require.Error(t, err)

	// loading the complete chain succeeds
	require.NoError(t, krw.Write(append(testutils.ECDSACertChain[0], testutils.ECDSACertChain[1]...), testutils.ECDSACertChainKeys[0], nil))
	secConfig, err := ca.LoadSecurityConfig(context.Background(), rootCA, krw, false)
	require.NoError(t, err)
	require.NotNil(t, secConfig)

	// set up a GRPC server using these credentials
	secConfig.ServerTLSCreds.Config().ClientAuth = tls.RequireAndVerifyClientCert
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverOpts := []grpc.ServerOption{grpc.Creds(secConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(serverOpts...)
	go grpcServer.Serve(l)
	defer grpcServer.Stop()

	// we should be able to connect to the server using the client credentials
	dialOpts := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(secConfig.ClientTLSCreds),
	}
	conn, err := grpc.Dial(l.Addr().String(), dialOpts...)
	require.NoError(t, err)
	conn.Close()
}

func TestSecurityConfigUpdateRootCA(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	tcConfig, err := tc.NewNodeConfig("worker")
	require.NoError(t, err)

	// create the "original" security config, and we'll update it to trust the test server's
	cert, key, err := testutils.CreateRootCertAndKey("root1")
	require.NoError(t, err)
	rootCA, err := ca.NewRootCA(cert, key, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	tempdir, err := ioutil.TempDir("", "test-security-config-update")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	configPaths := ca.NewConfigPaths(tempdir)

	secConfig, err := rootCA.CreateSecurityConfig(context.Background(),
		ca.NewKeyReadWriter(configPaths.Node, nil, nil), ca.CertificateRequestConfig{})
	require.NoError(t, err)
	// update the server TLS to require certificates, otherwise this will all pass
	// even if the root pools aren't updated
	secConfig.ServerTLSCreds.Config().ClientAuth = tls.RequireAndVerifyClientCert

	// set up a GRPC server using these credentials
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	serverOpts := []grpc.ServerOption{grpc.Creds(secConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(serverOpts...)
	go grpcServer.Serve(l)
	defer grpcServer.Stop()

	// we should not be able to connect to the test CA server using the original security config, and should not
	// be able to connect to new server using the test CA's client credentials
	dialOptsBase := []grpc.DialOption{
		grpc.WithBlock(),
		grpc.WithTimeout(10 * time.Second),
	}
	dialOpts := append(dialOptsBase, grpc.WithTransportCredentials(secConfig.ClientTLSCreds))
	_, err = grpc.Dial(tc.Addr, dialOpts...)
	require.Error(t, err)
	require.IsType(t, x509.UnknownAuthorityError{}, err)

	dialOpts = append(dialOptsBase, grpc.WithTransportCredentials(tcConfig.ClientTLSCreds))
	_, err = grpc.Dial(l.Addr().String(), dialOpts...)
	require.Error(t, err)
	require.IsType(t, x509.UnknownAuthorityError{}, err)

	// we can't connect to the test CA's external server either
	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)
	req := ca.PrepareCSR(csr, "cn", ca.ManagerRole, secConfig.ClientTLSCreds.Organization())

	externalServer := tc.ExternalSigningServer
	if testutils.External {
		// stop the external server and create a new one because the external server actually has to trust our client certs as well.
		updatedRoot, err := ca.NewRootCA(append(tc.RootCA.Cert, cert...), tc.RootCA.Signer.Key, ca.DefaultNodeCertExpiration, nil)
		require.NoError(t, err)
		externalServer, err = testutils.NewExternalSigningServer(updatedRoot, tc.TempDir)
		require.NoError(t, err)
		defer externalServer.Stop()

		secConfig.ExternalCA().UpdateURLs(externalServer.URL)
		_, err = secConfig.ExternalCA().Sign(context.Background(), req)
		require.Error(t, err)
		// the type is weird (it's wrapped in a bunch of other things in ctxhttp), so just compare strings
		require.Contains(t, err.Error(), x509.UnknownAuthorityError{}.Error())
	}

	// update the root CA on the "original"" security config to support both the old root
	// and the "new root" (the testing CA root)
	err = secConfig.UpdateRootCA(append(rootCA.Cert, tc.RootCA.Cert...), rootCA.Signer.Key, ca.DefaultNodeCertExpiration)
	require.NoError(t, err)

	// can now connect to the test CA using our modified security config, and can cannect to our server using
	// the test CA config
	conn, err := grpc.Dial(tc.Addr, dialOpts...)
	require.NoError(t, err)
	conn.Close()

	dialOpts = append(dialOptsBase, grpc.WithTransportCredentials(secConfig.ClientTLSCreds))
	conn, err = grpc.Dial(tc.Addr, dialOpts...)
	require.NoError(t, err)
	conn.Close()

	// we can also now connect to the test CA's external signing server
	if testutils.External {
		secConfig.ExternalCA().UpdateURLs(externalServer.URL)
		_, err := secConfig.ExternalCA().Sign(context.Background(), req)
		require.NoError(t, err)
	}
}

// enforce that no matter what order updating the root CA and updating TLS credential happens, we
// end up with a security config that has updated certs, and an updated root pool
func TestRenewTLSConfigUpdateRootCARace(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	paths := ca.NewConfigPaths(tc.TempDir)

	secConfig, err := tc.WriteNewNodeConfig(ca.WorkerRole)
	require.NoError(t, err)

	leafCert, err := ioutil.ReadFile(paths.Node.Cert)
	require.NoError(t, err)

	for i := 0; i < 5; i++ {
		cert, _, err := testutils.CreateRootCertAndKey(fmt.Sprintf("root %d", i+2))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		done1, done2 := make(chan struct{}), make(chan struct{})
		rootCA := secConfig.RootCA()
		go func() {
			defer close(done1)
			var key []byte
			if rootCA.Signer != nil {
				key = rootCA.Signer.Key
			}
			require.NoError(t, secConfig.UpdateRootCA(append(rootCA.Cert, cert...), key, ca.DefaultNodeCertExpiration))
		}()

		go func() {
			defer close(done2)
			require.NoError(t, ca.RenewTLSConfigNow(ctx, secConfig, tc.ConnBroker))
		}()

		<-done1
		<-done2

		newCert, err := ioutil.ReadFile(paths.Node.Cert)
		require.NoError(t, err)

		require.NotEqual(t, newCert, leafCert)
		leafCert = newCert

		// at the start of this loop had i+1 certs, afterward should have added one more
		require.Len(t, secConfig.ClientTLSCreds.Config().RootCAs.Subjects(), i+2)
		require.Len(t, secConfig.ServerTLSCreds.Config().RootCAs.Subjects(), i+2)
	}
}

func TestRenewTLSConfigWorker(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	// Because of the default backdate of 5 minutes, this issues certificates
	// valid for 1 minute.
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Signer.Key, ca.DefaultNodeCertExpiration, nil)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, key, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 1 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	err = ioutils.AtomicWriteFile(tc.Paths.Node.Key, key, 0600)
	assert.NoError(t, err)

	renew := make(chan struct{})
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.ConnBroker, renew)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.NoError(t, certUpdate.Err)
		assert.NotNil(t, certUpdate)
		assert.Equal(t, ca.WorkerRole, certUpdate.Role)
	}
}

func TestRenewTLSConfigManager(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	// Because of the default backdate of 5 minutes, this issues certificates
	// valid for 1 minute.
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Signer.Key, ca.DefaultNodeCertExpiration, nil)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, key, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 1 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	err = ioutils.AtomicWriteFile(tc.Paths.Node.Key, key, 0600)
	assert.NoError(t, err)

	// Get a new nodeConfig with a TLS cert that has 1 minute to live
	renew := make(chan struct{})

	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.ConnBroker, renew)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.NoError(t, certUpdate.Err)
		assert.NotNil(t, certUpdate)
		assert.Equal(t, ca.ManagerRole, certUpdate.Role)
	}
}

func TestRenewTLSConfigWithNoNode(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates.
	// Because of the default backdate of 5 minutes, this issues certificates
	// valid for 1 minute.
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Signer.Key, ca.DefaultNodeCertExpiration, nil)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, key, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 1 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	err = ioutils.AtomicWriteFile(tc.Paths.Node.Key, key, 0600)
	assert.NoError(t, err)

	// Delete the node from the backend store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, nodeConfig.ClientTLSCreds.NodeID())
		assert.NotNil(t, node)
		return store.DeleteNode(tx, nodeConfig.ClientTLSCreds.NodeID())
	})
	assert.NoError(t, err)

	renew := make(chan struct{})
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.ConnBroker, renew)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.Error(t, certUpdate.Err)
		assert.Contains(t, certUpdate.Err.Error(), "not found when attempting to renew certificate")
	}
}

func TestForceRenewTLSConfig(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new managerConfig with a TLS cert that has 15 minutes to live
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	renew := make(chan struct{}, 1)
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.ConnBroker, renew)
	renew <- struct{}{}
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestForceRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.NoError(t, certUpdate.Err)
		assert.NotNil(t, certUpdate)
		assert.Equal(t, certUpdate.Role, ca.ManagerRole)
	}
}
