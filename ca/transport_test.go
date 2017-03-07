package ca_test

import (
	"crypto/tls"
	"testing"

	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/stretchr/testify/assert"
)

func TestNewMutableTLS(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	cert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN", ca.ManagerRole, tc.Organization)
	assert.NoError(t, err)

	tlsConfig, err := ca.NewServerTLSConfig([]tls.Certificate{*cert}, tc.RootCA.Pool)
	assert.NoError(t, err)
	creds, err := ca.NewMutableTLS(tlsConfig)
	assert.NoError(t, err)
	assert.Equal(t, ca.ManagerRole, creds.Role())
	assert.Equal(t, "CN", creds.NodeID())
}

func TestGetAndValidateCertificateSubject(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	cert, err := tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN", ca.ManagerRole, tc.Organization)
	assert.NoError(t, err)

	name, err := ca.GetAndValidateCertificateSubject([]tls.Certificate{*cert})
	assert.NoError(t, err)
	assert.Equal(t, "CN", name.CommonName)
	assert.Len(t, name.OrganizationalUnit, 1)
	assert.Equal(t, ca.ManagerRole, name.OrganizationalUnit[0])
}

func TestLoadNewTLSConfig(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Create two different certs and two different TLS configs
	cert1, err := tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN1", ca.ManagerRole, tc.Organization)
	assert.NoError(t, err)
	cert2, err := tc.RootCA.IssueAndSaveNewCertificates(tc.KeyReadWriter, "CN2", ca.WorkerRole, tc.Organization)
	assert.NoError(t, err)
	tlsConfig1, err := ca.NewServerTLSConfig([]tls.Certificate{*cert1}, tc.RootCA.Pool)
	assert.NoError(t, err)
	tlsConfig2, err := ca.NewServerTLSConfig([]tls.Certificate{*cert2}, tc.RootCA.Pool)
	assert.NoError(t, err)

	// Load the first TLS config into a MutableTLS
	creds, err := ca.NewMutableTLS(tlsConfig1)
	assert.NoError(t, err)
	assert.Equal(t, ca.ManagerRole, creds.Role())
	assert.Equal(t, "CN1", creds.NodeID())

	// Load the new Config and assert it changed
	err = creds.LoadNewTLSConfig(tlsConfig2)
	assert.NoError(t, err)
	assert.Equal(t, ca.WorkerRole, creds.Role())
	assert.Equal(t, "CN2", creds.NodeID())
}
