package ca_test

import (
	"context"
	"crypto/x509"
	"testing"

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

	cert2, key2, err := testutils.CreateRootCertAndKey("rootCN2")
	require.NoError(t, err)

	rootCA2, err := ca.NewRootCA(cert2, key2, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	krw := ca.NewKeyReadWriter(paths.Node, nil, nil)

	_, err = rootCA2.IssueAndSaveNewCertificates(krw, "cn", "ou", "org")
	require.NoError(t, err)
	certBytes, _, err := krw.Read()
	require.NoError(t, err)
	leafCert, err := helpers.ParseCertificatePEM(certBytes)
	require.NoError(t, err)

	// we have not enabled CA signing on the external server
	_, err = externalCA.CrossSignRootCA(context.Background(), rootCA2)
	require.Error(t, err)

	require.NoError(t, tc.ExternalSigningServer.EnableCASigning())

	intermediate, err := externalCA.CrossSignRootCA(context.Background(), rootCA2)
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
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: tc.RootCA.Pool})
	require.Error(t, err)
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: tc.RootCA.Pool, Intermediates: intermediatePool})
	require.NoError(t, err)

	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool})
	require.NoError(t, err)
	_, err = leafCert.Verify(x509.VerifyOptions{Roots: rootCA2.Pool, Intermediates: intermediatePool})
	require.NoError(t, err)
}
