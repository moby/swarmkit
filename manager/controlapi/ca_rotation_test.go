package controlapi

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"
	"time"

	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/ca/testutils"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/moby/swarmkit/v2/log"
)

type rootCARotationTestCase struct {
	rootCA   *api.RootCA
	caConfig *api.CAConfig

	// what to expect if the validate and update succeeds - we can't always check that everything matches, for instance if
	// random values for join tokens or cross signed certs, or generated root rotation cert/key,
	// are expected
	expectRootCA                *api.RootCA
	expectJoinTokenChange       bool
	expectGeneratedRootRotation bool
	expectGeneratedCross        bool
	description                 string // in case an expectation fails

	// what error string to expect if the validate fails
	expectErrorString string
}

var initialLocalRootCA = &api.RootCA{
	CaCert:     testutils.ECDSA256SHA256Cert,
	CaKey:      testutils.ECDSA256Key,
	CaCertHash: "DEADBEEF",
	JoinTokens: &api.JoinTokens{
		Worker:  "SWMTKN-1-worker",
		Manager: "SWMTKN-1-manager",
	},
}
var rotationCert, rotationKey = testutils.ECDSACertChain[2], testutils.ECDSACertChainKeys[2]

func uglifyOnePEM(pemBytes []byte) []byte {
	pemBlock, _ := pem.Decode(pemBytes)
	pemBlock.Headers = map[string]string{
		"this": "should",
		"be":   "removed",
	}
	return append(append([]byte("\n\t   "), pem.EncodeToMemory(pemBlock)...), []byte("   \t")...)
}

func getSecurityConfig(t *testing.T, localRootCA *ca.RootCA, cluster *api.Cluster) *ca.SecurityConfig {
	t.Helper()
	tempdir := t.TempDir()
	paths := ca.NewConfigPaths(tempdir)
	secConfig, cancel, err := localRootCA.CreateSecurityConfig(context.Background(), ca.NewKeyReadWriter(paths.Node, nil, nil), ca.CertificateRequestConfig{})
	require.NoError(t, err)
	assert.NoError(t, cancel())
	return secConfig
}

func TestValidateCAConfigInvalidValues(t *testing.T) {
	t.Parallel()
	localRootCA, err := ca.NewRootCA(initialLocalRootCA.CaCert, initialLocalRootCA.CaCert, initialLocalRootCA.CaKey,
		ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	initialExternalRootCA := initialLocalRootCA.Copy()
	initialExternalRootCA.CaKey = nil

	crossSigned, err := localRootCA.CrossSignCACertificate(rotationCert)
	require.NoError(t, err)

	initExternalRootCAWithRotation := initialExternalRootCA.Copy()
	initExternalRootCAWithRotation.RootRotation = &api.RootRotation{
		CaCert:            rotationCert,
		CaKey:             rotationKey,
		CrossSignedCaCert: crossSigned,
	}

	// Copy: initialLocalRootCA is a shared package-level message now, so
	// mutating it here would corrupt every other test in this package.
	initWithExternalRootRotation := initialLocalRootCA.Copy()
	initWithExternalRootRotation.RootRotation = &api.RootRotation{
		CaCert:            rotationCert,
		CrossSignedCaCert: crossSigned,
	}

	// set up 2 external CAs that can be contacted for signing
	tempdir := t.TempDir()
	initExtServer, err := testutils.NewExternalSigningServer(localRootCA, tempdir)
	require.NoError(t, err)
	defer initExtServer.Stop()

	// we need to accept client certs from the original cert
	rotationRootCA, err := ca.NewRootCA(append(initialLocalRootCA.CaCert, rotationCert...), rotationCert, rotationKey,
		ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	rotateExtServer, err := testutils.NewExternalSigningServer(rotationRootCA, tempdir)
	require.NoError(t, err)
	defer rotateExtServer.Stop()

	for _, invalid := range []rootCARotationTestCase{
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaKey: initialLocalRootCA.CaKey,
			},
			expectErrorString: "the signing CA cert must also be provided",
		},
		{
			rootCA: initExternalRootCAWithRotation, // even if a root rotation is already in progress, the current CA external URL must be present
			caConfig: &api.CAConfig{
				ExternalCas: []*api.ExternalCA{
					{
						Url:      initExtServer.URL,
						CaCert:   initialLocalRootCA.CaCert,
						Protocol: 3, // wrong protocol
					},
					{
						Url:    initExtServer.URL,
						CaCert: rotationCert, // wrong cert
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the current CA certificate",
		},
		{
			rootCA: initialExternalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: rotationCert, // even if there's a desired cert, the current CA external URL must be present
				ExternalCas: []*api.ExternalCA{ // right certs, but invalid URLs in several ways
					{
						Url:    rotateExtServer.URL,
						CaCert: initialExternalRootCA.CaCert,
					},
					{
						Url:    "invalidurl",
						CaCert: initialExternalRootCA.CaCert,
					},
					{
						Url:    "https://too:many:colons:1:2:3",
						CaCert: initialExternalRootCA.CaCert,
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the current CA certificate",
		},
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: rotationCert,
				ExternalCas: []*api.ExternalCA{
					{
						Url:      rotateExtServer.URL,
						CaCert:   rotationCert,
						Protocol: 3, // wrong protocol
					},
					{
						Url: rotateExtServer.URL,
						// wrong cert because no cert is assumed to be the current root CA cert
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the desired CA certificate",
		},
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: rotationCert,
				ExternalCas: []*api.ExternalCA{ // right certs, but invalid URLs in several ways
					{
						Url:    initExtServer.URL,
						CaCert: rotationCert,
					},
					{
						Url:    "invalidurl",
						CaCert: rotationCert,
					},
					{
						Url:    "https://too:many:colons:1:2:3",
						CaCert: initialExternalRootCA.CaCert,
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the desired CA certificate",
		},
		{
			rootCA: initWithExternalRootRotation,
			caConfig: &api.CAConfig{ // no forceRotate change, no explicit signing cert change
				ExternalCas: []*api.ExternalCA{
					{
						Url:      rotateExtServer.URL,
						CaCert:   rotationCert,
						Protocol: 3, // wrong protocol
					},
					{
						Url:    rotateExtServer.URL,
						CaCert: initialLocalRootCA.CaCert, // wrong cert
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the next CA certificate",
		},
		{
			rootCA: initWithExternalRootRotation,
			caConfig: &api.CAConfig{ // no forceRotate change, no explicit signing cert change
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: rotationCert,
						// right CA cert, but the server cert is not signed by this CA cert
					},
					{
						Url:    "invalidurl",
						CaCert: rotationCert,
						// right CA cert, but invalid URL
					},
				},
			},
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the next CA certificate",
		},
		{
			rootCA:            initialExternalRootCA,
			caConfig:          &api.CAConfig{}, // removing the current external CA is not supported
			expectErrorString: "there must be at least one valid, reachable external CA corresponding to the current CA certificate",
		},
		{
			rootCA: initialExternalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: rotationCert,
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: initialLocalRootCA.CaCert, // current cert
					},
					{
						Url:    rotateExtServer.URL,
						CaCert: rotationCert, // new cert
					},
				},
			},
			expectErrorString: "rotating from one external CA to a different external CA is not supported",
		},
		{
			rootCA: initialExternalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: rotationCert,
				ExternalCas: []*api.ExternalCA{
					{
						Url: initExtServer.URL,
						// no cert means the current cert
					},
					{
						Url:    rotateExtServer.URL,
						CaCert: rotationCert, // new cert
					},
				},
			},
			expectErrorString: "rotating from one external CA to a different external CA is not supported",
		},
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: append(rotationCert, initialLocalRootCA.CaCert...),
				SigningCaKey:  rotationKey,
			},
			expectErrorString: "cannot contain multiple certificates",
		},
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: testutils.ReDateCert(t, rotationCert, rotationCert, rotationKey,
					time.Now().Add(-1*time.Minute), time.Now().Add(364*helpers.OneDay)),
				SigningCaKey: rotationKey,
			},
			expectErrorString: "expires too soon",
		},
		{
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: initialLocalRootCA.CaCert,
				SigningCaKey:  testutils.ExpiredKey, // same cert but mismatching key
			},
			expectErrorString: "certificate key mismatch",
		},
		{
			// this is just one class of failures caught by NewRootCA, not going to bother testing others, since they are
			// extensively tested in NewRootCA
			rootCA: initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: testutils.ExpiredCert,
				SigningCaKey:  testutils.ExpiredKey,
			},
			expectErrorString: "expired",
		},
	} {
		cluster := &api.Cluster{
			RootCa: invalid.rootCA,
			Spec: &api.ClusterSpec{
				CaConfig: invalid.caConfig,
			},
		}
		secConfig := getSecurityConfig(t, &localRootCA, cluster)
		_, err := validateCAConfig(context.Background(), secConfig, cluster)
		require.Error(t, err, invalid.expectErrorString)
		s, _ := status.FromError(err)
		require.Equal(t, codes.InvalidArgument, s.Code(), invalid.expectErrorString)
		require.Contains(t, s.Message(), invalid.expectErrorString)
	}
}

func runValidTestCases(t *testing.T, testcases []*rootCARotationTestCase, localRootCA *ca.RootCA) {
	logrus.SetLevel(logrus.DebugLevel)
	logrus.SetOutput(os.Stdout)
	ctx := log.WithLogger(context.Background(), log.L.WithField("testname", t.Name()))
	for _, valid := range testcases {
		casectx := log.WithField(ctx, "testcase", valid.description)
		cluster := &api.Cluster{
			RootCa: valid.rootCA.Copy(),
			Spec: &api.ClusterSpec{
				CaConfig: valid.caConfig,
			},
		}
		secConfig := getSecurityConfig(t, localRootCA, cluster)
		result, err := validateCAConfig(casectx, secConfig, cluster)
		require.NoError(t, err, valid.description)

		// ensure that the cluster was not mutated
		require.Equal(t, valid.rootCA, cluster.RootCa)

		// Because join tokens are random, we can't predict exactly what it is, so this needs to be manually checked
		if valid.expectJoinTokenChange {
			require.NotEmpty(t, result.JoinTokens, valid.rootCA.JoinTokens, valid.description)
		} else {
			require.Equal(t, result.JoinTokens, valid.rootCA.JoinTokens, valid.description)
		}
		result.JoinTokens = valid.expectRootCA.JoinTokens

		// If a cross-signed certificates is generated, we cant know what it is ahead of time.  All we can do is check that it's
		// correctly generated.
		if valid.expectGeneratedCross || valid.expectGeneratedRootRotation { // both generate cross signed certs
			require.NotNil(t, result.RootRotation, valid.description)
			require.NotEmpty(t, result.RootRotation.CrossSignedCaCert, valid.description)

			// make sure the cross-signed cert is signed by the current root CA (and not an intermediate, if a root rotation is in progress)
			parsedCross, err := helpers.ParseCertificatePEM(result.RootRotation.CrossSignedCaCert) // there should just be one
			require.NoError(t, err)

			log.G(casectx).Debugf("localRootCA:%s", localRootCA.Certs)
			log.G(casectx).Debugf("CACert:%s", result.RootRotation.CaCert)
			log.G(casectx).Debugf("CrossSigned:%s", result.RootRotation.CrossSignedCaCert)
			_, err = parsedCross.Verify(x509.VerifyOptions{Roots: localRootCA.Pool})
			assert.NoError(t, err, valid.description)

			// if we are expecting generated certs or root rotation, we can expect the expected root CA has a root rotation
			result.RootRotation.CrossSignedCaCert = valid.expectRootCA.RootRotation.CrossSignedCaCert
		}

		// If a root rotation cert is generated, we can't assert what the cert and key are.  So if we expect it to be generated,
		// just assert that the value has changed.
		if valid.expectGeneratedRootRotation {
			require.NotNil(t, result.RootRotation, valid.description)
			require.NotEqual(t, valid.rootCA.RootRotation, result.RootRotation, valid.description)
			result.RootRotation = valid.expectRootCA.RootRotation
		}

		require.True(t, result.EqualVT(valid.expectRootCA), valid.description)
	}
}

func printCert(t *testing.T, pemData []byte) {
	t.Helper()

	block, _ := pem.Decode(pemData)
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Error(err)
	}

	cert.RawSubject = nil
	cert.Raw = nil
	cert.RawIssuer = nil
	cert.RawSubjectPublicKeyInfo = nil
	cert.RawTBSCertificate = nil
	cert.Signature = nil
	t.Logf("%+v", cert)
}

func TestValidateCAConfigValidValues(t *testing.T) {
	t.Parallel()
	localRootCA, err := ca.NewRootCA(testutils.ECDSA256SHA256Cert, testutils.ECDSA256SHA256Cert, testutils.ECDSA256Key,
		ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)

	parsedKey, err := helpers.ParsePrivateKeyPEM(testutils.ECDSA256Key)
	require.NoError(t, err)

	initialExternalRootCA := initialLocalRootCA.Copy()
	initialExternalRootCA.CaKey = nil

	// set up 2 external CAs that can be contacted for signing
	tempdir := t.TempDir()
	initExtServer, err := testutils.NewExternalSigningServer(localRootCA, tempdir)
	require.NoError(t, err)
	defer initExtServer.Stop()
	require.NoError(t, initExtServer.EnableCASigning())

	// we need to accept client certs from the original cert
	rotationRootCA, err := ca.NewRootCA(append(initialLocalRootCA.CaCert, rotationCert...), rotationCert, rotationKey,
		ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	rotateExtServer, err := testutils.NewExternalSigningServer(rotationRootCA, tempdir)
	require.NoError(t, err)
	defer rotateExtServer.Stop()
	require.NoError(t, rotateExtServer.EnableCASigning())

	getExpectedRootCA := func(hasKey bool) *api.RootCA {
		result := initialLocalRootCA.Copy()
		result.LastForcedRotation = 5
		result.JoinTokens = &api.JoinTokens{}
		if !hasKey {
			result.CaKey = nil
		}
		return result
	}
	getRootCAWithRotation := func(base *api.RootCA, cert, key, cross []byte) *api.RootCA {
		init := base.Copy()
		init.RootRotation = &api.RootRotation{
			CaCert:            cert,
			CaKey:             key,
			CrossSignedCaCert: cross,
		}
		return init
	}

	// no change in the CAConfig spec means no rotation
	runValidTestCases(t, []*rootCARotationTestCase{
		{
			description:  "no specified config changes results no root rotation",
			rootCA:       initialLocalRootCA,
			caConfig:     &api.CAConfig{},
			expectRootCA: initialLocalRootCA,
		},
	}, &localRootCA)

	// These require no rotation, because the cert is exactly the same or there is no change specified.
	testcases := []*rootCARotationTestCase{
		{
			description: "same desired cert and key as current Root CA results in no root rotation",
			rootCA:      initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: uglifyOnePEM(initialLocalRootCA.CaCert),
				SigningCaKey:  initialLocalRootCA.CaKey,
				ForceRotate:   5,
			},
			expectRootCA: getExpectedRootCA(true),
		},
		{
			description: "same desired cert as current Root CA but external->internal (remove external CA is ok) results in no root rotation and no key -> key",
			rootCA:      initialExternalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: uglifyOnePEM(initialLocalRootCA.CaCert),
				SigningCaKey:  initialLocalRootCA.CaKey,
				ForceRotate:   5,
			},
			expectRootCA: getExpectedRootCA(true),
		},
		{
			description: "same desired cert as current Root CA but internal->external results in no root rotation and key -> no key",
			rootCA:      initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: initialLocalRootCA.CaCert,
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: uglifyOnePEM(initialLocalRootCA.CaCert),
					},
				},
				ForceRotate: 5,
			},
			expectRootCA: getExpectedRootCA(false),
		},
		{
			description: "same desired cert and key as current Root CA but adding an external CA results in no root rotation and no key change",
			rootCA:      initialLocalRootCA,
			caConfig: &api.CAConfig{
				SigningCaCert: initialLocalRootCA.CaCert,
				SigningCaKey:  initialLocalRootCA.CaKey,
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: uglifyOnePEM(initialLocalRootCA.CaCert),
					},
				},
				ForceRotate: 5,
			},
			expectRootCA: getExpectedRootCA(true),
		},
	}
	runValidTestCases(t, testcases, &localRootCA)

	// These are the same test cases as above, but we are testing that it will abort root rotation because
	// the desired cert is the same as the current RootCA cert
	crossSigned, err := localRootCA.CrossSignCACertificate(rotationCert)
	require.NoError(t, err)
	for _, testcase := range testcases {
		testcase.rootCA = getRootCAWithRotation(testcase.rootCA, rotationCert, rotationKey, crossSigned)
	}
	testcases[0].description = "same desired cert and key as current RootCA results in aborting root rotation"
	testcases[1].description = "same desired cert as current Root CA but external->internal (remove external CA is ok) results in aborting root rotation and no key -> key"
	testcases[2].description = "same desired cert, even if internal->external, as current RootCA results in aborting root rotation and key -> no key"
	testcases[3].description = "same desired cert and key as current Root CA but adding an external CA results in aborting root rotation and no key change"
	runValidTestCases(t, testcases, &localRootCA)

	// These will not change the root rotation because the desired cert is the same as the current to-be-rotated-to cert
	expectedBaseRootCA := getExpectedRootCA(true) // the main root CA expected will always have a signing key
	testcases = []*rootCARotationTestCase{
		{
			description: "same desired cert and key as current root rotation results in no change in root rotation",
			rootCA:      getRootCAWithRotation(initialLocalRootCA, rotationCert, rotationKey, crossSigned),
			caConfig: &api.CAConfig{
				SigningCaCert: testutils.ECDSACertChain[2],
				SigningCaKey:  testutils.ECDSACertChainKeys[2],
				ForceRotate:   5,
			},
			expectRootCA: getRootCAWithRotation(expectedBaseRootCA, rotationCert, rotationKey, crossSigned),
		},
		{
			description: "same desired cert as current root rotation but external->internal results minor change in root rotation (no key -> key)",
			rootCA:      getRootCAWithRotation(initialLocalRootCA, rotationCert, nil, crossSigned),
			caConfig: &api.CAConfig{
				SigningCaCert: testutils.ECDSACertChain[2],
				SigningCaKey:  testutils.ECDSACertChainKeys[2],
				ForceRotate:   5,
			},
			expectRootCA: getRootCAWithRotation(expectedBaseRootCA, rotationCert, rotationKey, crossSigned),
		},
		{
			description: "same desired cert as current root rotation but internal->external results minor change in root rotation (key -> no key)",
			rootCA:      getRootCAWithRotation(initialLocalRootCA, rotationCert, rotationKey, crossSigned),
			caConfig: &api.CAConfig{
				SigningCaCert: testutils.ECDSACertChain[2],
				ForceRotate:   5,
				ExternalCas: []*api.ExternalCA{
					{
						Url:    rotateExtServer.URL,
						CaCert: append(testutils.ECDSACertChain[2], ' '),
					},
				},
			},
			expectRootCA: getRootCAWithRotation(expectedBaseRootCA, rotationCert, nil, crossSigned),
		},
	}
	runValidTestCases(t, testcases, &localRootCA)

	// These all require a new root rotation because the desired cert is different, even if it has the same key and/or subject as the current
	// cert or the current-to-be-rotated cert.
	time.Sleep(5 * time.Second)
	parsedRotationCert, err := helpers.ParseCertificatePEM(rotationCert)
	require.NoError(t, err)
	parsedRotationKey, err := helpers.ParsePrivateKeyPEM(rotationKey)
	require.NoError(t, err)
	renewedRotationCert, err := initca.RenewFromSigner(parsedRotationCert, parsedRotationKey)
	require.NoError(t, err)
	differentInitialCert, err := testutils.CreateCertFromSigner("otherRootCN", parsedKey)
	require.NoError(t, err)
	differentRootCA, err := ca.NewRootCA(append(initialLocalRootCA.CaCert, differentInitialCert...), differentInitialCert,
		initialLocalRootCA.CaKey, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	differentExtServer, err := testutils.NewExternalSigningServer(differentRootCA, tempdir)
	require.NoError(t, err)
	defer differentExtServer.Stop()
	require.NoError(t, differentExtServer.EnableCASigning())
	testcases = []*rootCARotationTestCase{
		{
			description: "desired cert being a renewed rotation RootCA cert + rotation key results in replaced root rotation because the cert has changed",
			rootCA:      getRootCAWithRotation(initialLocalRootCA, rotationCert, rotationKey, crossSigned),
			caConfig: &api.CAConfig{
				SigningCaCert: uglifyOnePEM(renewedRotationCert),
				SigningCaKey:  rotationKey,
				ForceRotate:   5,
			},
			expectRootCA:         getRootCAWithRotation(expectedBaseRootCA, renewedRotationCert, rotationKey, nil),
			expectGeneratedCross: true,
		},
		{
			description: "desired cert being a different rotation rootCA cert results in replaced root rotation (only new external CA required, not old rotation external CA)",
			rootCA:      getRootCAWithRotation(initialLocalRootCA, rotationCert, nil, crossSigned),
			caConfig: &api.CAConfig{
				SigningCaCert: uglifyOnePEM(differentInitialCert),
				ForceRotate:   5,
				ExternalCas: []*api.ExternalCA{
					{
						// we need a different external server, because otherwise the external server's cert will fail to validate
						// (not signed by the right cert - note that there's a bug in go 1.7 where this is not needed, because the
						// subject names of cert names aren't checked, but go 1.8 fixes this.)
						Url:    differentExtServer.URL,
						CaCert: append([]byte("\n\t"), differentInitialCert...),
					},
				},
			},
			expectRootCA:         getRootCAWithRotation(expectedBaseRootCA, differentInitialCert, nil, nil),
			expectGeneratedCross: true,
		},
	}
	runValidTestCases(t, testcases, &localRootCA)

	// These require rotation because the cert and key are generated and hence completely different.
	testcases = []*rootCARotationTestCase{
		{
			description:                 "generating cert and key results in root rotation",
			rootCA:                      initialLocalRootCA,
			caConfig:                    &api.CAConfig{ForceRotate: 5},
			expectRootCA:                getRootCAWithRotation(getExpectedRootCA(true), nil, nil, nil),
			expectGeneratedRootRotation: true,
		},
		{
			description: "generating cert for external->internal results in root rotation",
			rootCA:      initialExternalRootCA,
			caConfig: &api.CAConfig{
				ForceRotate: 5,
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: uglifyOnePEM(initialExternalRootCA.CaCert),
					},
				},
			},
			expectRootCA:                getRootCAWithRotation(getExpectedRootCA(false), nil, nil, nil),
			expectGeneratedRootRotation: true,
		},
		{
			description:                 "generating cert and key results in replacing root rotation",
			rootCA:                      getRootCAWithRotation(initialLocalRootCA, rotationCert, rotationKey, crossSigned),
			caConfig:                    &api.CAConfig{ForceRotate: 5},
			expectRootCA:                getRootCAWithRotation(getExpectedRootCA(true), nil, nil, nil),
			expectGeneratedRootRotation: true,
		},
		{
			description:                 "generating cert and key results in replacing root rotation; external CAs required by old root rotation are no longer necessary",
			rootCA:                      getRootCAWithRotation(initialLocalRootCA, rotationCert, nil, crossSigned),
			caConfig:                    &api.CAConfig{ForceRotate: 5},
			expectRootCA:                getRootCAWithRotation(getExpectedRootCA(true), nil, nil, nil),
			expectGeneratedRootRotation: true,
		},
	}
	runValidTestCases(t, testcases, &localRootCA)

	// These require no change at all because the force rotate value hasn't changed, and there is no desired cert specified
	testcases = []*rootCARotationTestCase{
		{
			description:  "no desired certificate specified, no force rotation: no change to internal signer root (which has no outstanding rotation)",
			rootCA:       initialLocalRootCA,
			expectRootCA: initialLocalRootCA,
		},
		{
			description: "no desired certificate specified, no force rotation: no change to external CA root (which has no outstanding rotation)",
			rootCA:      initialExternalRootCA,
			caConfig: &api.CAConfig{
				ExternalCas: []*api.ExternalCA{
					{
						Url:    initExtServer.URL,
						CaCert: uglifyOnePEM(initialExternalRootCA.CaCert),
					},
				},
			},
			expectRootCA: initialExternalRootCA,
		},
	}
	runValidTestCases(t, testcases, &localRootCA)

	for _, testcase := range testcases {
		testcase.rootCA = getRootCAWithRotation(testcase.rootCA, rotationCert, rotationKey, crossSigned)
		testcase.expectRootCA = testcase.rootCA
	}
	testcases[0].description = "no desired certificate specified, no force rotation: no change to internal signer root or to outstanding rotation"
	testcases[1].description = "no desired certificate specified, no force rotation: no change to external CA root or to outstanding rotation"
	runValidTestCases(t, testcases, &localRootCA)
}
