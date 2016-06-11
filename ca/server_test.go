package ca_test

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
	"golang.org/x/net/context"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func TestGetRootCACertificate(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	resp, err := tc.CAClients[0].GetRootCACertificate(context.Background(), &api.GetRootCACertificateRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
}

func TestIssueNodeCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	role := api.NodeRoleWorker
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestIssueNodeCertificateWithInvalidCSR(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	role := api.NodeRoleWorker
	issueRequest := &api.IssueNodeCertificateRequest{CSR: []byte("random garbage"), Role: role}
	issueResponse, err := tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateFailed, statusResponse.Status.State)
	assert.Contains(t, statusResponse.Status.Err, "CSR Decode failed")
	assert.Nil(t, statusResponse.Certificate.Certificate)
}

func TestIssueNodeCertificateAgentRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	role := api.NodeRoleWorker
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[1].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[1].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestIssueNodeCertificateManagerRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestIssueNodeCertificateAgentFromDifferentOrgRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Since we're using a client that has a different Organization, this request will be treated
	// as a new certificate request, not allowing auto-renewal
	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[3].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotNil(t, issueResponse.NodeID)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		found := false
		for _, node := range storeNodes {
			if node.ID == issueResponse.NodeID {
				found = true
				assert.Equal(t, api.IssuanceStatePending, node.Certificate.Status.State)
			}
		}
		assert.True(t, found)
	})

}

func TestNodeCertificateAccept(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	testNode := &api.Node{
		ID: "nodeID",
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipAccepted,
			Role:       api.NodeRoleWorker,
		},
		Certificate: api.Certificate{
			CN:     "nodeID",
			CSR:    csr,
			Status: api.IssuanceStatus{State: api.IssuanceStatePending},
		},
	}

	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNode(tx, testNode))
		return nil
	})
	assert.NoError(t, err)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: "nodeID"}
	resp, err := tc.NodeCAClients[1].NodeCertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
	assert.NotEmpty(t, resp.Status)
	assert.NotNil(t, resp.Certificate.Certificate)
	assert.Equal(t, api.IssuanceStateIssued, resp.Status.State)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		var found bool
		for _, node := range storeNodes {
			if node.ID == "nodeID" {
				assert.Equal(t, api.IssuanceStateIssued, node.Certificate.Status.State)
				found = true
			}
		}
		assert.True(t, found)
	})
}

func TestNodeCertificateWithEmptyPolicies(t *testing.T) {
	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{},
	}
	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	testNode := &api.Node{
		ID: "nodeID",
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipAccepted,
		},
		Certificate: api.Certificate{
			CN:     "nodeID",
			CSR:    csr,
			Status: api.IssuanceStatus{State: api.IssuanceStatePending},
		},
	}

	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateNode(tx, testNode))
		return nil
	})
	assert.NoError(t, err)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: "nodeID"}
	resp, err := tc.NodeCAClients[1].NodeCertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
	assert.NotEmpty(t, resp.Status)
	assert.NotNil(t, resp.Certificate.Certificate)
	assert.Equal(t, api.IssuanceStateIssued, resp.Status.State)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		assert.Equal(t, api.IssuanceStatePending, storeNodes[0].Certificate.Status.State)
	})
}

func TestNodeCertificateRenewalsDoNotRequireSecret(t *testing.T) {
	hashPwd, _ := bcrypt.GenerateFromPassword([]byte("secret-data"), 0)

	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
			{
				Role:       api.NodeRoleWorker,
				Autoaccept: true,
				Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
					Data: hashPwd,
					Alg:  "bcrypt",
				},
			},
			{
				Role:       api.NodeRoleManager,
				Autoaccept: true,
				Secret: &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
					Data: hashPwd,
					Alg:  "bcrypt",
				},
			},
		},
	}

	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err = tc.NodeCAClients[1].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest = &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err = tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
}

func TestNewNodeCertificateRequiresSecret(t *testing.T) {
	agentHashPwd, _ := bcrypt.GenerateFromPassword([]byte("secret-data-worker"), 0)
	managerHashPwd, _ := bcrypt.GenerateFromPassword([]byte("secret-data-manager"), 0)

	agentSecret := &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
		Data: agentHashPwd,
		Alg:  "bcrypt",
	}
	managerSecret := &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
		Data: managerHashPwd,
		Alg:  "bcrypt",
	}

	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
			{
				Role:       api.NodeRoleWorker,
				Autoaccept: true,
				Secret:     agentSecret,
			},
			{
				Role:       api.NodeRoleManager,
				Autoaccept: true,
				Secret:     managerSecret,
			},
		},
	}

	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issuance fails if no secret is provided
	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: invalid policy or secret")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: invalid policy or secret")

	// Issuance fails if wrong secret is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: crypto/bcrypt: hashedPassword is not the hash of the given password")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: crypto/bcrypt: hashedPassword is not the hash of the given password")

	// Issuance fails if secret for the other role is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-worker"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: crypto/bcrypt: hashedPassword is not the hash of the given password")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-manager"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: crypto/bcrypt: hashedPassword is not the hash of the given password")
	// Issuance succeeds if correct secret is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-manager"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-worker"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
}
func TestNewNodeCertificateBadSecret(t *testing.T) {
	managerHashPwd, _ := bcrypt.GenerateFromPassword([]byte("secret-data-manager"), 0)

	badSecret := &api.AcceptancePolicy_RoleAdmissionPolicy_HashedSecret{
		Data: managerHashPwd,
		Alg:  "sha1",
	}
	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
			{
				Role:       api.NodeRoleWorker,
				Autoaccept: true,
				Secret:     badSecret,
			},
			{
				Role:       api.NodeRoleManager,
				Autoaccept: true,
				Secret:     badSecret,
			},
		},
	}

	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issuance fails if wrong secret is provided
	role := api.NodeRoleManager
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: hash algorithm not supported: sha1")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster: hash algorithm not supported: sha1")
}
