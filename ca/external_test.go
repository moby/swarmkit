package ca_test

import (
	"context"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/stretchr/testify/require"
)

// Tests ExternalCA.CrossSignRootCA can produce an intermediate that can be used to
// validate a leaf certificate
func TestExternalCACrossSign(t *testing.T) {
	t.Parallel()

	if !testutils.External {
		return // this is only tested using the external CA
	}

	tc := testutils.NewTestCA(t)
	defer tc.Stop()
	paths := ca.NewConfigPaths(tc.TempDir)

	secConfig, err := tc.RootCA.CreateSecurityConfig(context.Background(),
		ca.NewKeyReadWriter(paths.Node, nil, nil), ca.CertificateRequestConfig{})
	require.NoError(t, err)
	externalCA := secConfig.ExternalCA()
	externalCA.UpdateURLs(tc.ExternalSigningServer.URL)

	for _, testcase := range []struct{ cert, key []byte }{
		{
			cert: testutils.ECDSA256SHA256Cert,
			key:  testutils.ECDSA256Key,
		},
		{
			cert: testutils.RSA2048SHA256Cert,
			key:  testutils.RSA2048Key,
		},
	} {
		rootCA2, err := ca.NewRootCA(testcase.cert, testcase.cert, testcase.key, ca.DefaultNodeCertExpiration, nil)
		require.NoError(t, err)

		krw := ca.NewKeyReadWriter(paths.Node, nil, nil)

		_, _, err = rootCA2.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
		require.NoError(t, err)
		certBytes, _, err := krw.Read()
		require.NoError(t, err)
		leafCert, err := helpers.ParseCertificatePEM(certBytes)
		require.NoError(t, err)

		// we have not enabled CA signing on the external server
		tc.ExternalSigningServer.DisableCASigning()
		_, err = externalCA.CrossSignRootCA(context.Background(), rootCA2)
		require.Error(t, err)

		require.NoError(t, tc.ExternalSigningServer.EnableCASigning())

		intermediate, err := externalCA.CrossSignRootCA(context.Background(), rootCA2)
		require.NoError(t, err)

		parsedIntermediate, err := helpers.ParseCertificatePEM(intermediate)
		require.NoError(t, err)
		parsedRoot2, err := helpers.ParseCertificatePEM(testcase.cert)
		require.NoError(t, err)
		require.Equal(t, parsedRoot2.RawSubject, parsedIntermediate.RawSubject)
		require.Equal(t, parsedRoot2.RawSubjectPublicKeyInfo, parsedIntermediate.RawSubjectPublicKeyInfo)
		require.True(t, parsedIntermediate.IsCA)

		intermediatePool := x509.NewCertPool()
		intermediatePool.AddCert(parsedIntermediate)

		// we can validate a chain from the leaf to the first root through the intermediate,
		// or from the leaf cert to the second root with or without the intermediate
		_, err = leafCert.Verify(x509.VerifyOptions{Roots: tc.RootCA.Pool})
		require.Error(t, err)
		_, err = leafCert.Verify(x509.VerifyOptions{Roots: tc.RootCA.Pool, Intermediates: intermediatePool})
		require.NoError(t, err)

		_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool})
		require.NoError(t, err)
		_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool, Intermediates: intermediatePool})
		require.NoError(t, err)
	}
}

func TestExternalCASignRequestTimesOut(t *testing.T) {
	t.Parallel()

	if testutils.External {
		return // this does not require the external CA in any way
	}

	rootCA, err := ca.CreateRootCA("rootCN")
	require.NoError(t, err)

	signDone, allDone := make(chan error), make(chan struct{})
	defer close(signDone)
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(http.ResponseWriter, *http.Request) {
		// hang forever
		select {
		case <-allDone:
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()
	defer server.CloseClientConnections()
	defer close(allDone)

	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)

	externalCA := ca.NewExternalCA(&rootCA, nil, server.URL)
	externalCA.ExternalRequestTimeout = time.Second
	go func() {
		_, err := externalCA.Sign(context.Background(), ca.PrepareCSR(csr, "cn", "ou", "org"))
		select {
		case <-allDone:
		case signDone <- err:
		}
	}()

	select {
	case err = <-signDone:
		require.Contains(t, err.Error(), context.DeadlineExceeded.Error())
	case <-time.After(3 * time.Second):
		require.FailNow(t, "call to external CA signing should have timed out after 1 second - it's been 3")
	}
}

func TestExternalCACopy(t *testing.T) {
	t.Parallel()

	if !testutils.External {
		return // this is only tested using the external CA
	}

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	require.NoError(t, err)
	signReq := ca.PrepareCSR(csr, "cn", ca.ManagerRole, tc.Organization)

	secConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	require.NoError(t, err)
	externalCA1 := secConfig.ExternalCA()
	externalCA2 := externalCA1.Copy()
	externalCA2.UpdateURLs(tc.ExternalSigningServer.URL)

	// externalCA1 can't sign, but externalCA2, which has been updated with URLS, can
	_, err = externalCA1.Sign(context.Background(), signReq)
	require.Equal(t, ca.ErrNoExternalCAURLs, err)

	cert, err := externalCA2.Sign(context.Background(), signReq)
	require.NoError(t, err)
	require.NotNil(t, cert)
}
