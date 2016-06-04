package ca_test

import (
	"testing"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/manager/state/store"
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

	role := ca.AgentRole
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

	role := ca.AgentRole
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

	role := ca.ManagerRole
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)
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
		assert.Equal(t, api.IssuanceStateIssued, storeNodes[0].Certificate.Status.State)
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
		assert.Equal(t, api.IssuanceStateRejected, storeNodes[0].Certificate.Status.State)
	})

}

func TestNodeCertificateRenewalsDoNotRequireSecret(t *testing.T) {
	policy := api.AcceptancePolicy{
		Autoaccept: map[string]bool{ca.AgentRole: true},
		Secret:     "secret-data",
	}

	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	role := ca.ManagerRole
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.NodeCAClients[2].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.NodeID)

	statusRequest := &api.NodeCertificateStatusRequest{NodeID: issueResponse.NodeID}
	statusResponse, err := tc.NodeCAClients[2].NodeCertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.Certificate.Certificate)
	assert.Equal(t, role, statusResponse.Certificate.Role)

	role = ca.AgentRole
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
		Autoaccept: map[string]bool{},
		Secret:     "secret-data",
	}

	tc := testutils.NewTestCA(t, policy)
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issuance fails if no secret is provided
	role := ca.ManagerRole
	issueRequest := &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 2 desc = A valid secret token is necessary to join this cluster")

	role = ca.AgentRole
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 2 desc = A valid secret token is necessary to join this cluster")

	// Issuance fails if wrong secret is provided
	role = ca.ManagerRole
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "data-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 2 desc = A valid secret token is necessary to join this cluster")

	role = ca.AgentRole
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "data-secret"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.EqualError(t, err, "rpc error: code = 2 desc = A valid secret token is necessary to join this cluster")

	// Issuance succeeds if correct secret is provided
	role = ca.ManagerRole
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)

	role = ca.AgentRole
	issueRequest = &api.IssueNodeCertificateRequest{CSR: csr, Role: role, Secret: "secret-data"}
	_, err = tc.NodeCAClients[0].IssueNodeCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
}
