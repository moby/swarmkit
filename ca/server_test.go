package ca_test

import (
	"testing"

	"golang.org/x/net/context"

	"github.com/docker/libswarm/api"
	"github.com/docker/libswarm/ca"
	"github.com/docker/libswarm/ca/testutils"
	"github.com/docker/libswarm/manager/state/store"
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
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[0].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
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

func TestNodeCertificateReject(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	testNode := &api.Node{
		ID: "nodeID",
		Spec: api.NodeSpec{
			Membership: api.NodeMembershipRejected,
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
	assert.Nil(t, resp.Certificate.Certificate)
	assert.Equal(t, api.IssuanceStateRejected, resp.Status.State)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeNodes, err := store.FindNodes(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeNodes)
		var found bool
		for _, node := range storeNodes {
			if node.ID == "nodeID" {
				assert.Equal(t, api.IssuanceStateRejected, node.Certificate.Status.State)
				found = true
			}
		}
		assert.True(t, found)
	})
}

func TestNodeCertificateRenewalsDoNotRequireSecret(t *testing.T) {
	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
			{
				Role:       api.NodeRoleWorker,
				Autoaccept: true,
				Secret:     "secret-data",
			},
			{
				Role:       api.NodeRoleManager,
				Autoaccept: true,
				Secret:     "secret-data",
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
	policy := api.AcceptancePolicy{
		Policies: []*api.AcceptancePolicy_RoleAdmissionPolicy{
			{
				Role:       api.NodeRoleWorker,
				Autoaccept: true,
				Secret:     "secret-data-worker",
			},
			{
				Role:       api.NodeRoleManager,
				Autoaccept: true,
				Secret:     "secret-data-manager",
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
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")

	// Issuance fails if wrong secret is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "invalid-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")

	// Issuance fails if secret for the other role is provided
	role = api.NodeRoleManager
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-worker"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")

	role = api.NodeRoleWorker
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data-manager"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 3 desc = A valid secret token is necessary to join this cluster")
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
