package ca_test

import (
	"bytes"
	"crypto/x509"
	"fmt"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	raftutils "github.com/docker/swarmkit/manager/state/raft/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

var _ api.CAServer = &ca.Server{}
var _ api.NodeCAServer = &ca.Server{}

func TestGetRootCACertificate(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	resp, err := tc.CAClients[0].GetRootCACertificate(context.Background(), &api.GetRootCACertificateRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
}

func TestRestartRootCA(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	_, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), &api.NodeCertificateStatusRequest{NodeID: "foo"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))

	tc.CAServer.Stop()
	go tc.CAServer.Run(context.Background())

	<-tc.CAServer.Ready()

	_, err = tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), &api.NodeCertificateStatusRequest{NodeID: "foo"})
	assert.Error(t, err)
	assert.Equal(t, codes.NotFound, grpc.Code(err))
}

func TestIssueNodeCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Token: tc.WorkerToken}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, api.NodeRoleWorker, statusResponse.Certificate.Role)
}

func TestForceRotationIsNoop(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Get a new Certificate issued
	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Token: tc.WorkerToken}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	// Check that the Certificate is successfully issued
	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, api.NodeRoleWorker, statusResponse.Certificate.Role)

	// Update the certificate status to IssuanceStateRotate which should be a server-side noop
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		// Attempt to retrieve the node with nodeID
		node := store.GetNode(tx, issueResponse.NodeID)
		assert.NotNil(t, node)

		node.Certificate.Status.State = api.IssuanceStateRotate
		return store.UpdateNode(tx, node)
	})
	assert.NoError(t, err)

	// Wait a bit and check that the certificate hasn't changed/been reissued
	time.Sleep(250 * time.Millisecond)

	statusNewResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, statusResponse.Certificate.Certificate, statusNewResponse.Certificate.Certificate)
	assert.Equal(t, api.IssuanceStateRotate, statusNewResponse.Certificate.Status.State)
	assert.Equal(t, api.NodeRoleWorker, statusNewResponse.Certificate.Role)
}

func TestIssueNodeCertificateBrokenCA(t *testing.T) {
	if !testutils.External {
		t.Skip("test only applicable for external CA configuration")
	}

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	tc.ExternalSigningServer.Flake()

	go func() {
		time.Sleep(250 * time.Millisecond)
		tc.ExternalSigningServer.Deflake()
	}()
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Token: tc.WorkerToken}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, api.NodeRoleWorker, statusResponse.Certificate.Role)

}

func TestIssueNodeCertificateWithInvalidCSR(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	issueRequest := &api.IssueNodeCertificateRequest{CSR: []byte("random garbage"), Token: tc.WorkerToken}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateFailed, statusResponse.Status.State)
	assert.Contains(t, statusResponse.Status.Err, "CSR Decode failed")
	assert.Nil(t, statusResponse.Certificate.Certificate)
}

func TestIssueNodeCertificateWorkerRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	role := api.NodeRoleWorker
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[1].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[1].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestIssueNodeCertificateManagerRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	require.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestIssueNodeCertificateWorkerFromDifferentOrgRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Since we're using a client that has a different Organization, this request will be treated
	// as a new certificate request, not allowing auto-renewal. Therefore, the request will fail.
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr}
	_, err = tc.NodeCAClients[3].IssueNodeCertificate(context.Background(), issueRequest)
	assert.Error(t, err)
}

func TestNodeCertificateRenewalsDoNotRequireToken(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err = tc.NodeCAClients[1].IssueNodeCertificate(context.Background(), issueRequest)
	require.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)
	assert.Equal(t, api.NodeMembershipAccepted, issueResponse.NodeMembership)

	statusRequest = &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err = tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	require.NoError(t, err)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestNewNodeCertificateRequiresToken(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Issuance fails if no secret is provided
	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	// Issuance fails if wrong secret is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	// Issuance succeeds if correct token is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: tc.ManagerToken}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: tc.WorkerToken}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)

	// Rotate manager and worker tokens
	var (
		newManagerToken string
		newWorkerToken  string
	)
	assert.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		clusters, _ := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
		newWorkerToken = ca.GenerateJoinToken(&tc.RootCA)
		clusters[0].RootCA.JoinTokens.Worker = newWorkerToken
		newManagerToken = ca.GenerateJoinToken(&tc.RootCA)
		clusters[0].RootCA.JoinTokens.Manager = newManagerToken
		return store.UpdateCluster(tx, clusters[0])
	}))

	// updating the join token may take a little bit in order to register on the CA server, so poll
	assert.NoError(t, raftutils.PollFunc(nil, func() error {
		// Old token should fail
		role = api.NodeRoleManager
		issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: tc.ManagerToken}
		_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
		if err == nil {
			return fmt.Errorf("join token not updated yet")
		}
		return nil
	}))

	// Old token should fail
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: tc.WorkerToken}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	// New token should succeed
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: newManagerToken}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: newWorkerToken}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
}

func TestNewNodeCertificateBadToken(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	csr, _, err := ca.GenerateNewCSR()
	assert.NoError(t, err)

	// Issuance fails if wrong secret is provided
	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Token: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid join token is necessary to join this cluster")
}

func TestGetUnlockKey(t *testing.T) {
	t.Parallel()

	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	var cluster *api.Cluster
	tc.MemoryStore.View(func(tx store.ReadTx) {
		clusters, err := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
		require.NoError(t, err)
		cluster = clusters[0]
	})

	resp, err := tc.CAClients[0].GetUnlockKey(context.Background(), &api.GetUnlockKeyRequest{})
	require.NoError(t, err)
	require.Nil(t, resp.UnlockKey)
	require.Equal(t, cluster.Meta.Version, resp.Version)

	// Update the unlock key
	require.NoError(t, tc.MemoryStore.Update(func(tx store.Tx) error {
		cluster = store.GetCluster(tx, cluster.ID)
		cluster.Spec.EncryptionConfig.AutoLockManagers = true
		cluster.UnlockKeys = []*api.EncryptionKey{{
			Subsystem: ca.ManagerRole,
			Key:       []byte("secret"),
		}}
		return store.UpdateCluster(tx, cluster)
	}))

	tc.MemoryStore.View(func(tx store.ReadTx) {
		cluster = store.GetCluster(tx, cluster.ID)
	})

	require.NoError(t, raftutils.PollFuncWithTimeout(nil, func() error {
		resp, err = tc.CAClients[0].GetUnlockKey(context.Background(), &api.GetUnlockKeyRequest{})
		if err != nil {
			return fmt.Errorf("get unlock key: %v", err)
		}
		if !bytes.Equal(resp.UnlockKey, []byte("secret")) {
			return fmt.Errorf("secret hasn't rotated yet")
		}
		if cluster.Meta.Version.Index > resp.Version.Index {
			return fmt.Errorf("hasn't updated to the right version yet")
		}
		return nil
	}, 250*time.Millisecond))
}

type clusterObjToUpdate struct {
	clusterObj           *api.Cluster
	rootCARoots          []byte
	rootCASigningCert    []byte
	rootCASigningKey     []byte
	rootCAIntermediates  []byte
	externalCertSignedBy []byte
}

func TestCAServerUpdateRootCA(t *testing.T) {
	// this one needs both external CA servers for testing
	if !testutils.External {
		return
	}

	fakeClusterSpec := func(rootCerts, key []byte, rotation *api.RootRotation, externalCAs []*api.ExternalCA) *api.Cluster {
		return &api.Cluster{
			RootCA: api.RootCA{
				CACert:     rootCerts,
				CAKey:      key,
				CACertHash: "hash",
				JoinTokens: api.JoinTokens{
					Worker:  "SWMTKN-1-worker",
					Manager: "SWMTKN-1-manager",
				},
				RootRotation: rotation,
			},
			Spec: api.ClusterSpec{
				CAConfig: api.CAConfig{
					ExternalCAs: externalCAs,
				},
			},
		}
	}

	tc := testutils.NewTestCA(t)
	require.NoError(t, tc.CAServer.Stop())
	defer tc.Stop()

	cert, key, err := testutils.CreateRootCertAndKey("new root to rotate to")
	require.NoError(t, err)
	newRootCA, err := ca.NewRootCA(append(tc.RootCA.Certs, cert...), cert, key, ca.DefaultNodeCertExpiration, nil)
	require.NoError(t, err)
	externalServer, err := testutils.NewExternalSigningServer(newRootCA, tc.TempDir)
	require.NoError(t, err)
	defer externalServer.Stop()
	crossSigned, err := tc.RootCA.CrossSignCACertificate(cert)
	require.NoError(t, err)

	for i, testCase := range []clusterObjToUpdate{
		{
			clusterObj: fakeClusterSpec(tc.RootCA.Certs, nil, nil, []*api.ExternalCA{{
				Protocol: api.ExternalCA_CAProtocolCFSSL,
				URL:      tc.ExternalSigningServer.URL,
				// without a CA cert, the URL gets successfully added, and there should be no error connecting to it
			}}),
			rootCARoots:          tc.RootCA.Certs,
			externalCertSignedBy: tc.RootCA.Certs,
		},
		{
			clusterObj: fakeClusterSpec(tc.RootCA.Certs, nil, &api.RootRotation{
				CACert:            cert,
				CAKey:             key,
				CrossSignedCACert: crossSigned,
			}, []*api.ExternalCA{
				{
					Protocol: api.ExternalCA_CAProtocolCFSSL,
					URL:      tc.ExternalSigningServer.URL,
					// without a CA cert, we count this as the old tc.RootCA.Certs, and this should be ignored because we want the new root
				},
			}),
			rootCARoots:         tc.RootCA.Certs,
			rootCASigningCert:   crossSigned,
			rootCASigningKey:    key,
			rootCAIntermediates: crossSigned,
		},
		{
			clusterObj: fakeClusterSpec(tc.RootCA.Certs, nil, &api.RootRotation{
				CACert:            cert,
				CrossSignedCACert: crossSigned,
			}, []*api.ExternalCA{
				{
					Protocol: api.ExternalCA_CAProtocolCFSSL,
					URL:      tc.ExternalSigningServer.URL,
					// without a CA cert, we count this as the old tc.RootCA.Certs
				},
				{
					Protocol: api.ExternalCA_CAProtocolCFSSL,
					URL:      externalServer.URL,
					CACert:   append(cert, '\n'),
				},
			}),
			rootCARoots:          tc.RootCA.Certs,
			rootCAIntermediates:  crossSigned,
			externalCertSignedBy: cert,
		},
	} {
		require.NoError(t, tc.CAServer.UpdateRootCA(context.Background(), testCase.clusterObj))

		rootCA := tc.ServingSecurityConfig.RootCA()
		require.Equal(t, testCase.rootCARoots, rootCA.Certs)
		var signingCert, signingKey []byte
		if s, err := rootCA.Signer(); err == nil {
			signingCert, signingKey = s.Cert, s.Key
		}
		require.Equal(t, testCase.rootCARoots, rootCA.Certs)
		require.Equal(t, testCase.rootCASigningCert, signingCert, "%d", i)
		require.Equal(t, testCase.rootCASigningKey, signingKey, "%d", i)
		require.Equal(t, testCase.rootCAIntermediates, rootCA.Intermediates)

		externalCA := tc.ServingSecurityConfig.ExternalCA()
		csr, _, err := ca.GenerateNewCSR()
		require.NoError(t, err)
		signedCert, err := externalCA.Sign(context.Background(), ca.PrepareCSR(csr, "cn", ca.ManagerRole, tc.Organization))

		if testCase.externalCertSignedBy != nil {
			require.NoError(t, err)
			parsed, err := helpers.ParseCertificatePEM(signedCert)
			require.NoError(t, err)
			rootPool := x509.NewCertPool()
			rootPool.AppendCertsFromPEM(testCase.externalCertSignedBy)
			_, err = parsed.Verify(x509.VerifyOptions{Roots: rootPool})
			require.NoError(t, err)
		} else {
			require.Equal(t, ca.ErrNoExternalCAURLs, err)
		}
	}
}
