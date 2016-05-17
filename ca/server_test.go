package ca

import (
	"testing"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func TestGetRootCACertificate(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	resp, err := tc.clients[0].GetRootCACertificate(context.Background(), &api.GetRootCACertificateRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
}

func TestIssueCertificate(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Agent)
	assert.NoError(t, err)

	var token string
	role := AgentRole
	completed := make(chan error)
	go func() {
		issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
		issueResponse, err := tc.clients[0].IssueCertificate(context.Background(), issueRequest)
		token = issueResponse.Token
		assert.NotNil(t, token)
		completed <- err
	}()

	assert.NoError(t, <-completed)

	go func() {
		statusRequest := &api.CertificateStatusRequest{Token: token}
		statusResponse, err := tc.clients[0].CertificateStatus(context.Background(), statusRequest)
		assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
		assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
		assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
		completed <- err
	}()

	assert.NoError(t, <-completed)
}

func TestIssueCertificateAgentRenewal(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Agent)
	assert.NoError(t, err)

	var token string
	role := AgentRole
	completed := make(chan error)
	go func() {
		issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
		issueResponse, err := tc.clients[1].IssueCertificate(context.Background(), issueRequest)
		token = issueResponse.Token
		assert.NotNil(t, token)
		completed <- err
	}()

	assert.NoError(t, <-completed)

	go func() {
		statusRequest := &api.CertificateStatusRequest{Token: token}
		statusResponse, err := tc.clients[1].CertificateStatus(context.Background(), statusRequest)
		assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
		assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
		assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
		completed <- err
	}()

	assert.NoError(t, <-completed)
}

func TestIssueCertificateManagerRenewal(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	var token string
	role := ManagerRole
	completed := make(chan error)
	go func() {
		issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
		issueResponse, err := tc.clients[2].IssueCertificate(context.Background(), issueRequest)
		token = issueResponse.Token
		assert.NotNil(t, token)
		completed <- err
	}()

	assert.NoError(t, <-completed)

	go func() {
		statusRequest := &api.CertificateStatusRequest{Token: token}
		statusResponse, err := tc.clients[2].CertificateStatus(context.Background(), statusRequest)
		assert.Equal(t, api.IssuanceStateIssued, statusResponse.Status.State)
		assert.NotNil(t, statusResponse.RegisteredCertificate.Certificate)
		assert.Equal(t, role, statusResponse.RegisteredCertificate.Role)
		completed <- err
	}()

	assert.NoError(t, <-completed)
}

func TestCertificateDesiredStateIssued(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
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

	err = tc.s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	tc.caServer.reconcileCertificates(context.Background(), []*api.RegisteredCertificate{testRegisteredCert})

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.NotNil(t, resp.RegisteredCertificate.Certificate)
	assert.Equal(t, api.IssuanceStateIssued, resp.Status.State)

	tc.s.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateIssued, storeCerts[0].Status.State)
	})
}

func TestCertificateDesiredStateBlocked(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
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

	err = tc.s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	tc.caServer.reconcileCertificates(context.Background(), []*api.RegisteredCertificate{testRegisteredCert})

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.Equal(t, api.IssuanceStateBlocked, resp.Status.State)
	assert.Nil(t, resp.RegisteredCertificate.Certificate)

	tc.s.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateBlocked, storeCerts[0].Status.State)
	})
}

func TestCertificateDesiredStateRejected(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
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

	err = tc.s.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	tc.caServer.reconcileCertificates(context.Background(), []*api.RegisteredCertificate{testRegisteredCert})

	statusRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := tc.clients[1].CertificateStatus(context.Background(), statusRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.Status)
	assert.Equal(t, api.IssuanceStateRejected, resp.Status.State)
	assert.Nil(t, resp.RegisteredCertificate.Certificate)

	tc.s.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateRejected, storeCerts[0].Status.State)
	})
}

func TestIssueAcceptedRegisteredCertificate(t *testing.T) {
	tc := NewTestCA(t, DefaultAcceptancePolicy())
	defer tc.Stop()

	csr, _, err := GenerateAndWriteNewCSR(tc.paths.Manager)
	assert.NoError(t, err)

	nodeID := identity.NewID()
	token := identity.NewID()
	role := ManagerRole
	resp, err := tc.caServer.issueAcceptedRegisteredCertificate(tc.ctx, nodeID, role, token, csr)
	assert.NoError(t, err)
	assert.NotNil(t, resp.Token)

	tc.s.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, api.IssuanceStateAccepted, storeCerts[0].Status.State)
		assert.Equal(t, role, storeCerts[0].Role)

	})
}
