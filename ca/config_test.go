package ca_test

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/Sirupsen/logrus"
	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/docker/swarmkit/watch"
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
	require.NotNil(t, rootCA.Certs)
	_, err = rootCA.Signer()
	require.Equal(t, err, ca.ErrNoValidSigner)
	require.Equal(t, tc.RootCA.Certs, rootCA.Certs)

	// Remove the CA cert
	os.RemoveAll(tc.Paths.RootCA.Cert)

	// downloading without a join token also succeeds
	rootCA, err = ca.DownloadRootCA(tc.Context, tc.Paths.RootCA, "", tc.ConnBroker)
	require.NoError(t, err)
	require.NotNil(t, rootCA.Pool)
	require.NotNil(t, rootCA.Certs)
	_, err = rootCA.Signer()
	require.Equal(t, err, ca.ErrNoValidSigner)
	require.Equal(t, tc.RootCA.Certs, rootCA.Certs)
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
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	assert.NoError(t, tc.CAServer.Stop())

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

	root, err := helpers.ParseCertificatePEM(tc.RootCA.Certs)
	assert.NoError(t, err)

	issuerInfo := nodeConfig.IssuerInfo()
	assert.NotNil(t, issuerInfo)
	assert.Equal(t, root.RawSubjectPublicKeyInfo, issuerInfo.PublicKey)
	assert.Equal(t, root.RawSubject, issuerInfo.Subject)
}

func TestCreateSecurityConfigNoCerts(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	root, err := helpers.ParseCertificatePEM(tc.RootCA.Certs)
	assert.NoError(t, err)

	validateNodeConfig := func(rootCA *ca.RootCA) {
		nodeConfig, err := rootCA.CreateSecurityConfig(tc.Context, krw,
			ca.CertificateRequestConfig{
				Token:      tc.WorkerToken,
				ConnBroker: tc.ConnBroker,
			})
		assert.NoError(t, err)
		assert.NotNil(t, nodeConfig)
		assert.NotNil(t, nodeConfig.ClientTLSCreds)
		assert.NotNil(t, nodeConfig.ServerTLSCreds)
		// tc.RootCA can maybe sign, and the node root CA can also maybe sign, so we want to just compare the root
		// certs and intermediates
		assert.Equal(t, tc.RootCA.Certs, nodeConfig.RootCA().Certs)
		assert.Equal(t, tc.RootCA.Intermediates, nodeConfig.RootCA().Intermediates)

		issuerInfo := nodeConfig.IssuerInfo()
		assert.NotNil(t, issuerInfo)
		assert.Equal(t, root.RawSubjectPublicKeyInfo, issuerInfo.PublicKey)
		assert.Equal(t, root.RawSubject, issuerInfo.Subject)
	}

	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are locally signed
	os.RemoveAll(tc.Paths.Node.Cert)
	validateNodeConfig(&tc.RootCA)

	// Remove only the node certificates form the directory, get a new rootCA, and attest that we get
	// new certificates that are issued by the remote CA
	os.RemoveAll(tc.Paths.Node.Cert)
	rootCA, err := ca.GetLocalRootCA(tc.Paths.RootCA)
	assert.NoError(t, err)
	validateNodeConfig(&rootCA)
}

func TestLoadSecurityConfigExpiredCert(t *testing.T) {
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	s, err := tc.RootCA.Signer()
	require.NoError(t, err)

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)
	now := time.Now()

	_, _, err = tc.RootCA.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)
	certBytes, _, err := krw.Read()
	require.NoError(t, err)

	// A cert that is not yet valid is not valid even if expiry is allowed
	invalidCert := testutils.ReDateCert(t, certBytes, tc.RootCA.Certs, s.Key, now.Add(time.Hour), now.Add(time.Hour*2))
	require.NoError(t, ioutil.WriteFile(tc.Paths.Node.Cert, invalidCert, 0700))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, true)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	// a cert that is expired is not valid if expiry is not allowed
	invalidCert = testutils.ReDateCert(t, certBytes, tc.RootCA.Certs, s.Key, now.Add(-2*time.Minute), now.Add(-1*time.Minute))
	require.NoError(t, ioutil.WriteFile(tc.Paths.Node.Cert, invalidCert, 0700))

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))

	// but it is valid if expiry is allowed
	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, true)
	require.NoError(t, err)
}

func TestLoadSecurityConfigInvalidCert(t *testing.T) {
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.Node.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)

	_, err := ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	assert.Error(t, err)
}

func TestLoadSecurityConfigInvalidKey(t *testing.T) {
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	krw := ca.NewKeyReadWriter(tc.Paths.Node, nil, nil)

	_, err := ca.LoadSecurityConfig(tc.Context, tc.RootCA, krw, false)
	assert.Error(t, err)
}

func TestLoadSecurityConfigIncorrectPassphrase(t *testing.T) {
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	paths := ca.NewConfigPaths(tc.TempDir)
	_, _, err := tc.RootCA.IssueAndSaveNewCertificates(ca.NewKeyReadWriter(paths.Node, []byte("kek"), nil),
		"nodeID", ca.WorkerRole, tc.Organization)
	require.NoError(t, err)

	_, err = ca.LoadSecurityConfig(tc.Context, tc.RootCA, ca.NewKeyReadWriter(paths.Node, nil, nil), false)
	require.IsType(t, ca.ErrInvalidKEK{}, err)
}

func TestLoadSecurityConfigIntermediates(t *testing.T) {
	if testutils.External {
		return // this doesn't require any servers at all
	}
	tempdir, err := ioutil.TempDir("", "test-load-config-with-intermediates")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	paths := ca.NewConfigPaths(tempdir)
	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)

	rootCA, err := ca.NewRootCA(testutils.ECDSACertChain[2], nil, nil, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	ctx := log.WithLogger(context.Background(), log.L.WithFields(logrus.Fields{
		"testname":          t.Name(),
		"testHasExternalCA": false,
	}))

	// loading the incomplete chain fails
	require.NoError(t, krw.Write(testutils.ECDSACertChain[0], testutils.ECDSACertChainKeys[0], nil))
	_, err = ca.LoadSecurityConfig(ctx, rootCA, krw, false)
	require.Error(t, err)

	intermediate, err := helpers.ParseCertificatePEM(testutils.ECDSACertChain[1])
	require.NoError(t, err)

	// loading the complete chain succeeds
	require.NoError(t, krw.Write(append(testutils.ECDSACertChain[0], testutils.ECDSACertChain[1]...), testutils.ECDSACertChainKeys[0], nil))
	secConfig, err := ca.LoadSecurityConfig(ctx, rootCA, krw, false)
	require.NoError(t, err)
	require.NotNil(t, secConfig)
	issuerInfo := secConfig.IssuerInfo()
	require.NotNil(t, issuerInfo)
	require.Equal(t, intermediate.RawSubjectPublicKeyInfo, issuerInfo.PublicKey)
	require.Equal(t, intermediate.RawSubject, issuerInfo.Subject)

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

// When the root CA is updated on the security config, the root pools are updated
// and the external CA's rootCA is also updated.
func TestSecurityConfigUpdateRootCA(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	tcConfig, err := tc.NewNodeConfig("worker")
	require.NoError(t, err)

	// create the "original" security config, and we'll update it to trust the test server's
	cert, key, err := testutils.CreateRootCertAndKey("root1")
	require.NoError(t, err)

	rootCA, err := ca.NewRootCA(cert, cert, key, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	tempdir, err := ioutil.TempDir("", "test-security-config-update")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)
	configPaths := ca.NewConfigPaths(tempdir)

	secConfig, err := rootCA.CreateSecurityConfig(tc.Context,
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
	tcSigner, err := tc.RootCA.Signer()
	require.NoError(t, err)
	if testutils.External {
		// stop the external server and create a new one because the external server actually has to trust our client certs as well.
		updatedRoot, err := ca.NewRootCA(append(tc.RootCA.Certs, cert...), tcSigner.Cert, tcSigner.Key, ca.DefaultNodeCertExpiration, nil)
		require.NoError(t, err)
		externalServer, err = testutils.NewExternalSigningServer(updatedRoot, tc.TempDir)
		require.NoError(t, err)
		defer externalServer.Stop()

		secConfig.ExternalCA().UpdateURLs(externalServer.URL)
		_, err = secConfig.ExternalCA().Sign(tc.Context, req)
		require.Error(t, err)
		// the type is weird (it's wrapped in a bunch of other things in ctxhttp), so just compare strings
		require.Contains(t, err.Error(), x509.UnknownAuthorityError{}.Error())
	}

	// update the root CA on the "original security config to support both the old root
	// and the "new root" (the testing CA root).  Also make sure this root CA has an
	// intermediate; we won't use it for anything, just make sure that newly generated TLS
	// certs have the intermediate appended.
	someOtherRootCA, err := ca.CreateRootCA("someOtherRootCA")
	require.NoError(t, err)
	intermediate, err := someOtherRootCA.CrossSignCACertificate(cert)
	require.NoError(t, err)
	rSigner, err := rootCA.Signer()
	require.NoError(t, err)
	updatedRootCA, err := ca.NewRootCA(concat(rootCA.Certs, tc.RootCA.Certs, someOtherRootCA.Certs), rSigner.Cert, rSigner.Key, ca.DefaultNodeCertExpiration, intermediate)
	require.NoError(t, err)
	err = secConfig.UpdateRootCA(&updatedRootCA, updatedRootCA.Pool)
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

	// make sure any generated certs after updating contain the intermediate
	var generatedCert []byte
	if testutils.External {
		// we can also now connect to the test CA's external signing server
		secConfig.ExternalCA().UpdateURLs(externalServer.URL)
		generatedCert, err = secConfig.ExternalCA().Sign(tc.Context, req)
		require.NoError(t, err)
	} else {
		krw := ca.NewKeyReadWriter(configPaths.Node, nil, nil)
		_, _, err := secConfig.RootCA().IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
		require.NoError(t, err)
		generatedCert, _, err = krw.Read()
		require.NoError(t, err)
	}

	parsedCerts, err := helpers.ParseCertificatesPEM(generatedCert)
	require.NoError(t, err)
	require.Len(t, parsedCerts, 2)
	parsedIntermediate, err := helpers.ParseCertificatePEM(intermediate)
	require.NoError(t, err)
	require.Equal(t, parsedIntermediate, parsedCerts[1])
}

// You can't update the root CA to one that doesn't match the TLS certificates
func TestSecurityConfigUpdateRootCAUpdateConsistentWithTLSCertificates(t *testing.T) {
	t.Parallel()
	if testutils.External {
		return // we don't care about external CAs at all
	}
	tempdir, err := ioutil.TempDir("", "")
	require.NoError(t, err)
	krw := ca.NewKeyReadWriter(ca.NewConfigPaths(tempdir).Node, nil, nil)

	rootCA, err := ca.CreateRootCA("rootcn")
	require.NoError(t, err)
	tlsKeyPair, issuerInfo, err := rootCA.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)

	otherRootCA, err := ca.CreateRootCA("otherCN")
	require.NoError(t, err)
	_, otherIssuerInfo, err := otherRootCA.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)
	intermediate, err := rootCA.CrossSignCACertificate(otherRootCA.Certs)
	require.NoError(t, err)
	otherTLSCert, otherTLSKey, err := krw.Read()
	require.NoError(t, err)
	otherTLSKeyPair, err := tls.X509KeyPair(append(otherTLSCert, intermediate...), otherTLSKey)
	require.NoError(t, err)

	// Note - the validation only happens on UpdateRootCA for now, because the assumption is
	// that something else does the validation when loading the security config for the first
	// time and when getting new TLS credentials

	secConfig, err := ca.NewSecurityConfig(&rootCA, krw, tlsKeyPair, issuerInfo)
	require.NoError(t, err)

	// can't update the root CA or external pool to one that doesn't match the tls certs
	require.Error(t, secConfig.UpdateRootCA(&otherRootCA, rootCA.Pool))
	require.Error(t, secConfig.UpdateRootCA(&rootCA, otherRootCA.Pool))

	// can update the secConfig's root CA to one that does match the certs
	combinedRootCA, err := ca.NewRootCA(append(otherRootCA.Certs, rootCA.Certs...), nil, nil,
		ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	require.NoError(t, secConfig.UpdateRootCA(&combinedRootCA, combinedRootCA.Pool))

	// if there are intermediates, we can update to a root CA that signed the intermediate
	require.NoError(t, secConfig.UpdateTLSCredentials(&otherTLSKeyPair, otherIssuerInfo))
	require.NoError(t, secConfig.UpdateRootCA(&rootCA, rootCA.Pool))

}

func TestSecurityConfigSetWatch(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	secConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	require.NoError(t, err)
	issuer := secConfig.IssuerInfo()

	w := watch.NewQueue()
	defer w.Close()
	secConfig.SetWatch(w)

	configWatch, configCancel := w.Watch()
	defer configCancel()

	require.NoError(t, ca.RenewTLSConfigNow(tc.Context, secConfig, tc.ConnBroker, tc.Paths.RootCA))
	select {
	case ev := <-configWatch:
		nodeTLSInfo, ok := ev.(*api.NodeTLSInfo)
		require.True(t, ok)
		require.Equal(t, &api.NodeTLSInfo{
			TrustRoot:           tc.RootCA.Certs,
			CertIssuerPublicKey: issuer.PublicKey,
			CertIssuerSubject:   issuer.Subject,
		}, nodeTLSInfo)
	case <-time.After(time.Second):
		require.FailNow(t, "on TLS certificate update, we should have gotten a security config update")
	}

	require.NoError(t, secConfig.UpdateRootCA(&tc.RootCA, tc.RootCA.Pool))
	select {
	case ev := <-configWatch:
		nodeTLSInfo, ok := ev.(*api.NodeTLSInfo)
		require.True(t, ok)
		require.Equal(t, &api.NodeTLSInfo{
			TrustRoot:           tc.RootCA.Certs,
			CertIssuerPublicKey: issuer.PublicKey,
			CertIssuerSubject:   issuer.Subject,
		}, nodeTLSInfo)
	case <-time.After(time.Second):
		require.FailNow(t, "on TLS certificate update, we should have gotten a security config update")
	}

	configCancel()
	w.Close()

	// ensure that we can still update tls certs and roots without error even though the watch is closed
	require.NoError(t, secConfig.UpdateRootCA(&tc.RootCA, tc.RootCA.Pool))
	require.NoError(t, ca.RenewTLSConfigNow(tc.Context, secConfig, tc.ConnBroker, tc.Paths.RootCA))
}

// If we get an unknown authority error when trying to renew the TLS certificate, attempt to download the
// root certificate.  If it validates against the current TLS credentials, it will be used to download
// new ones, (only if the new certificate indicates that it's a worker, though).
func TestRenewTLSConfigUpdatesRootOnUnknownAuthError(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-renew-tls-config-now-downloads-root")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// make 3 CAs
	var (
		certs        = make([][]byte, 3)
		keys         = make([][]byte, 3)
		crossSigneds = make([][]byte, 3)
		cas          = make([]ca.RootCA, 3)
	)
	for i := 0; i < 3; i++ {
		certs[i], keys[i], err = testutils.CreateRootCertAndKey(fmt.Sprintf("CA%d", i))
		require.NoError(t, err)
		switch i {
		case 0:
			crossSigneds[i] = nil
			cas[i], err = ca.NewRootCA(certs[i], certs[i], keys[i], ca.DefaultNodeCertExpiration, nil)
			require.NoError(t, err)
		default:
			crossSigneds[i], err = cas[i-1].CrossSignCACertificate(certs[i])
			require.NoError(t, err)
			cas[i], err = ca.NewRootCA(certs[i-1], certs[i], keys[i], ca.DefaultNodeCertExpiration, crossSigneds[i])
			require.NoError(t, err)
		}
	}

	// the CA server is going to start off with a cert issued by the second CA, cross-signed by the first CA, and then
	// rotate to one issued by the third CA, cross-signed by the second.
	tc := testutils.NewTestCAFromAPIRootCA(t, tempdir, api.RootCA{
		CACert: certs[0],
		CAKey:  keys[0],
		RootRotation: &api.RootRotation{
			CACert:            certs[1],
			CAKey:             keys[1],
			CrossSignedCACert: crossSigneds[1],
		},
	}, nil)
	defer tc.Stop()
	require.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		cluster := store.GetCluster(tx, tc.Organization)
		cluster.RootCA.CACert = certs[1]
		cluster.RootCA.CAKey = keys[1]
		cluster.RootCA.RootRotation = &api.RootRotation{
			CACert:            certs[2],
			CAKey:             keys[2],
			CrossSignedCACert: crossSigneds[2],
		}
		return store.UpdateCluster(tx, cluster)
	}))

	paths := ca.NewConfigPaths(tempdir)
	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)
	for i, testCase := range []struct {
		role          api.NodeRole
		initialRootCA *ca.RootCA
		issuingRootCA *ca.RootCA
		expectedRoot  []byte
	}{
		{
			role:          api.NodeRoleWorker,
			initialRootCA: &cas[0],
			issuingRootCA: &cas[1],
			expectedRoot:  certs[1],
		},
		{
			role:          api.NodeRoleManager,
			initialRootCA: &cas[0],
			issuingRootCA: &cas[1],
		},
		// TODO(cyli): once signing root CA and serving root CA for the CA server are split up, so that the server can accept
		// requests from certs different than the cluster root CA, add another test case to make sure that the downloaded
		// root has to validate against both the old TLS creds and new TLS creds
	} {
		nodeID := fmt.Sprintf("node%d", i)
		tlsKeyPair, issuerInfo, err := testCase.issuingRootCA.IssueAndSaveNewCertificates(krw, nodeID, ca.ManagerRole, tc.Organization)
		require.NoError(t, err)
		// make sure the node is added to the memory store as a worker, so when we renew the cert the test CA will answer
		require.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
			return store.CreateNode(tx, &api.Node{
				Role: testCase.role,
				ID:   nodeID,
				Spec: api.NodeSpec{
					DesiredRole:  testCase.role,
					Membership:   api.NodeMembershipAccepted,
					Availability: api.NodeAvailabilityActive,
				},
			})
		}))
		secConfig, err := ca.NewSecurityConfig(testCase.initialRootCA, krw, tlsKeyPair, issuerInfo)
		require.NoError(t, err)

		paths := ca.NewConfigPaths(filepath.Join(tempdir, nodeID))
		err = ca.RenewTLSConfigNow(tc.Context, secConfig, tc.ConnBroker, paths.RootCA)

		// TODO(cyli): remove this role check once the codepaths for worker and manager are the same
		if testCase.expectedRoot != nil {
			// only rotate if we are a worker, and if the new cert validates against the old TLS creds
			require.NoError(t, err)
			downloadedRoot, err := ioutil.ReadFile(paths.RootCA.Cert)
			require.NoError(t, err)
			require.Equal(t, testCase.expectedRoot, downloadedRoot)
		} else {
			require.Error(t, err)
			require.IsType(t, x509.UnknownAuthorityError{}, err)
			_, err = ioutil.ReadFile(paths.RootCA.Cert) // we didn't download a file
			require.Error(t, err)
		}
	}
}

// If we get a not unknown authority error when trying to renew the TLS certificate, just return the
// error and do not attempt to download the root certificate.
func TestRenewTLSConfigUpdatesRootNonUnknownAuthError(t *testing.T) {
	tempdir, err := ioutil.TempDir("", "test-renew-tls-config-now-downloads-root")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	cert, key, err := testutils.CreateRootCertAndKey("rootCA")
	require.NoError(t, err)
	rootCA, err := ca.NewRootCA(cert, cert, key, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	tc := testutils.NewTestCAFromAPIRootCA(t, tempdir, api.RootCA{
		CACert: cert,
		CAKey:  key,
	}, nil)
	defer tc.Stop()

	fakeCAServer := newNonSigningCAServer(t, tc)
	defer fakeCAServer.stop(t)

	secConfig, err := tc.NewNodeConfig(ca.WorkerRole)
	require.NoError(t, err)
	tc.CAServer.Stop()

	signErr := make(chan error)
	go func() {
		updates, cancel := state.Watch(tc.MemoryStore.WatchQueue(), api.EventCreateNode{})
		defer cancel()
		select {
		case event := <-updates: // we want to skip the first node, which is the test CA
			n := event.(api.EventCreateNode).Node
			if n.Certificate.Status.State == api.IssuanceStatePending {
				signErr <- tc.MemoryStore.Update(func(tx store.Tx) error {
					node := store.GetNode(tx, n.ID)
					certChain, err := rootCA.ParseValidateAndSignCSR(node.Certificate.CSR, node.Certificate.CN, ca.WorkerRole, tc.Organization)
					if err != nil {
						return err
					}
					node.Certificate.Certificate = testutils.ReDateCert(t, certChain, cert, key, time.Now().Add(-5*time.Hour), time.Now().Add(-4*time.Hour))
					node.Certificate.Status = api.IssuanceStatus{
						State: api.IssuanceStateIssued,
					}
					return store.UpdateNode(tx, node)
				})
				return
			}
		}
	}()

	err = ca.RenewTLSConfigNow(tc.Context, secConfig, fakeCAServer.getConnBroker(), tc.Paths.RootCA)
	require.Error(t, err)
	require.IsType(t, x509.CertificateInvalidError{}, errors.Cause(err))
	require.NoError(t, <-signErr)
}

// enforce that no matter what order updating the root CA and updating TLS credential happens, we
// end up with a security config that has updated certs, and an updated root pool
func TestRenewTLSConfigUpdateRootCARace(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	paths := ca.NewConfigPaths(tc.TempDir)

	secConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	require.NoError(t, err)

	leafCert, err := ioutil.ReadFile(paths.Node.Cert)
	require.NoError(t, err)

	cert, key, err := testutils.CreateRootCertAndKey("extra root cert for external CA")
	require.NoError(t, err)
	extraExternalRootCA, err := ca.NewRootCA(append(cert, tc.RootCA.Certs...), cert, key, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	extraExternalServer, err := testutils.NewExternalSigningServer(extraExternalRootCA, tc.TempDir)
	require.NoError(t, err)
	defer extraExternalServer.Stop()
	secConfig.ExternalCA().UpdateURLs(extraExternalServer.URL)

	externalPool := x509.NewCertPool()
	externalPool.AppendCertsFromPEM(tc.RootCA.Certs)
	externalPool.AppendCertsFromPEM(cert)

	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)
	signReq := ca.PrepareCSR(csr, "cn", ca.WorkerRole, tc.Organization)

	for i := 0; i < 5; i++ {
		cert, _, err := testutils.CreateRootCertAndKey(fmt.Sprintf("root %d", i+2))
		require.NoError(t, err)

		ctx, cancel := context.WithCancel(tc.Context)
		defer cancel()

		done1, done2 := make(chan struct{}), make(chan struct{})
		rootCA := secConfig.RootCA()
		go func() {
			defer close(done1)
			s := ca.LocalSigner{}
			if signer, err := rootCA.Signer(); err == nil {
				s = *signer
			}
			updatedRootCA, err := ca.NewRootCA(append(rootCA.Certs, cert...), s.Cert, s.Key, ca.DefaultNodeCertExpiration, nil)
			require.NoError(t, err)
			externalPool.AppendCertsFromPEM(cert)
			require.NoError(t, secConfig.UpdateRootCA(&updatedRootCA, externalPool))
		}()

		go func() {
			defer close(done2)
			require.NoError(t, ca.RenewTLSConfigNow(ctx, secConfig, tc.ConnBroker, tc.Paths.RootCA))
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
		// no matter what, the external CA still has the extra external CA root cert
		_, err = secConfig.ExternalCA().Sign(tc.Context, signReq)
		require.NoError(t, err)
	}
}

func writeAlmostExpiringCertToDisk(t *testing.T, tc *testutils.TestCA, cn, ou, org string) {
	s, err := tc.RootCA.Signer()
	require.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	// Because of the default backdate of 5 minutes, this issues certificates
	// valid for 1 minute.
	newRootCA, err := ca.NewRootCA(tc.RootCA.Certs, s.Cert, s.Key, ca.DefaultNodeCertExpiration, nil)
	assert.NoError(t, err)
	newSigner, err := newRootCA.Signer()
	require.NoError(t, err)
	newSigner.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Issue a new certificate with the same details as the current config, but with 1 min expiration time, and
	// overwrite the existing cert on disk
	_, _, err = newRootCA.IssueAndSaveNewCertificates(ca.NewKeyReadWriter(tc.Paths.Node, nil, nil), cn, ou, org)
	assert.NoError(t, err)
}

func TestRenewTLSConfigWorker(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(tc.Context)
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration, but overwrite
	// the cert on disk with one that expires in 1 minute
	nodeConfig, err := tc.WriteNewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)
	c := nodeConfig.ClientTLSCreds
	writeAlmostExpiringCertToDisk(t, tc, c.NodeID(), c.Role(), c.Organization())

	renewer := ca.NewTLSRenewer(nodeConfig, tc.ConnBroker, tc.Paths.RootCA)
	updates := renewer.Start(ctx)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.NoError(t, certUpdate.Err)
		assert.NotNil(t, certUpdate)
		assert.Equal(t, ca.WorkerRole, certUpdate.Role)
	}

	root, err := helpers.ParseCertificatePEM(tc.RootCA.Certs)
	assert.NoError(t, err)

	issuerInfo := nodeConfig.IssuerInfo()
	assert.NotNil(t, issuerInfo)
	assert.Equal(t, root.RawSubjectPublicKeyInfo, issuerInfo.PublicKey)
	assert.Equal(t, root.RawSubject, issuerInfo.Subject)
}

func TestRenewTLSConfigManager(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(tc.Context)
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration, but overwrite
	// the cert on disk with one that expires in 1 minute
	nodeConfig, err := tc.WriteNewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)
	c := nodeConfig.ClientTLSCreds
	writeAlmostExpiringCertToDisk(t, tc, c.NodeID(), c.Role(), c.Organization())

	renewer := ca.NewTLSRenewer(nodeConfig, tc.ConnBroker, tc.Paths.RootCA)
	updates := renewer.Start(ctx)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.NoError(t, certUpdate.Err)
		assert.NotNil(t, certUpdate)
		assert.Equal(t, ca.WorkerRole, certUpdate.Role)
	}

	root, err := helpers.ParseCertificatePEM(tc.RootCA.Certs)
	assert.NoError(t, err)

	issuerInfo := nodeConfig.IssuerInfo()
	assert.NotNil(t, issuerInfo)
	assert.Equal(t, root.RawSubjectPublicKeyInfo, issuerInfo.PublicKey)
	assert.Equal(t, root.RawSubject, issuerInfo.Subject)
}

func TestRenewTLSConfigWithNoNode(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(tc.Context)
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration, but overwrite
	// the cert on disk with one that expires in 1 minute
	nodeConfig, err := tc.WriteNewNodeConfig(ca.WorkerRole)
	assert.NoError(t, err)
	c := nodeConfig.ClientTLSCreds
	writeAlmostExpiringCertToDisk(t, tc, c.NodeID(), c.Role(), c.Organization())

	// Delete the node from the backend store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, nodeConfig.ClientTLSCreds.NodeID())
		assert.NotNil(t, node)
		return store.DeleteNode(tx, nodeConfig.ClientTLSCreds.NodeID())
	})
	assert.NoError(t, err)

	renewer := ca.NewTLSRenewer(nodeConfig, tc.ConnBroker, tc.Paths.RootCA)
	updates := renewer.Start(ctx)
	select {
	case <-time.After(10 * time.Second):
		assert.Fail(t, "TestRenewTLSConfig timed-out")
	case certUpdate := <-updates:
		assert.Error(t, certUpdate.Err)
		assert.Contains(t, certUpdate.Err.Error(), "not found when attempting to renew certificate")
	}
}
