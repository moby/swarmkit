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

	resp, err := tc.Clients[0].GetRootCACertificate(context.Background(), &api.GetRootCACertificateRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
}

func TestIssueCertificate(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	role := ca.AgentRole
	issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.Clients[0].IssueCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.Token)

	statusRequest := &api.CertificateStatusRequest{Token: issueResponse.Token}
	statusResponse, err := tc.Clients[0].CertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
	assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
}

func TestIssueCertificateAgentRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	role := ca.AgentRole
	issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.Clients[1].IssueCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.Token)

	statusRequest := &api.CertificateStatusRequest{Token: issueResponse.Token}
	statusResponse, err := tc.Clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
	assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
}

func TestIssueCertificateManagerRenewal(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	role := ca.ManagerRole
	issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := tc.Clients[2].IssueCertificate(context.Background(), issueRequest)
	assert.NotNil(t, issueResponse.Token)

	statusRequest := &api.CertificateStatusRequest{Token: issueResponse.Token}
	statusResponse, err := tc.Clients[2].CertificateStatus(context.Background(), statusRequest)
	assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
	assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
	assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
}

func TestCertificateDesiredStateIssued(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	testRegisteredCert := &api.RegisteredCertificate{
		ID:  "token",
		CN:  "cn",
		CSR: csr,
		Spec: api.RegisteredCertificateSpec{
			DesiredState: api.IssuanceStateIssued,
		},
		Status: api.IssuanceStatus{State: api.IssuanceStatePending},
	}

	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.Clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.NotNil(t, resp.RegisteredCertificate.Certificate)
	assert.Equal(t, api.IssuanceStateIssued, resp.Status.State)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateIssued, storeCerts[0].Status.State)
	})
}

func TestCertificateDesiredStateBlocked(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	testRegisteredCert := &api.RegisteredCertificate{
		ID:  "token",
		CN:  "cn",
		CSR: csr,
		Spec: api.RegisteredCertificateSpec{
			DesiredState: api.IssuanceStateBlocked,
		},
		Status: api.IssuanceStatus{State: api.IssuanceStatePending},
	}

	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.Clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.Equal(t, api.IssuanceStateBlocked, resp.Status.State)
	assert.Nil(t, resp.RegisteredCertificate.Certificate)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateBlocked, storeCerts[0].Status.State)
	})
}

func TestCertificateDesiredStateRejected(t *testing.T) {
	tc := testutils.NewTestCA(t, ca.DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := ca.GenerateAndWriteNewCSR(tc.Paths.Node)
	assert.NoError(t, err)

	testRegisteredCert := &api.RegisteredCertificate{
		ID:  "token",
		CN:  "cn",
		CSR: csr,
		Spec: api.RegisteredCertificateSpec{
			DesiredState: api.IssuanceStateRejected,
		},
		Status: api.IssuanceStatus{State: api.IssuanceStatePending},
	}

	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.Clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.Equal(t, api.IssuanceStateRejected, resp.Status.State)
	assert.Nil(t, resp.RegisteredCertificate.Certificate)

	tc.MemoryStore.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateRejected, storeCerts[0].Status.State)
	})
}
