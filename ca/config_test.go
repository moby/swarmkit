package ca_test

import (
	"io/ioutil"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/stretchr/testify/assert"
)

func TestLoadOrCreateSecurityConfigEmptyDir(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new node
	os.RemoveAll(tc.TempDir)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.Nil(t, nodeConfig.RootCA.Signer)
	assert.False(t, nodeConfig.RootCA.CanSign())
}

func TestLoadOrCreateSecurityConfigNoCerts(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Remove only the node certificates form the directory, and attest that we get
	// new certificates
	os.RemoveAll(tc.Paths.Node.Cert)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.NotNil(t, nodeConfig.RootCA.Signer)
	assert.True(t, nodeConfig.RootCA.CanSign())
}

func TestLoadOrCreateSecurityConfigInvalidCACertNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, nil)
	assert.EqualError(t, err, "valid remote address picker required")
	assert.Nil(t, nodeConfig)
}

func TestLoadOrCreateSecurityConfigInvalidCACert(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// First load the current nodeConfig. We'll verify that after we corrupt
	// the certificate, another subsquent call with get us new certs
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	// We have a valid signer because we bootstrapped with valid root key-material
	assert.NotNil(t, nodeConfig.RootCA.Signer)
	assert.True(t, nodeConfig.RootCA.CanSign())

	// Write some garbage to the CA cert
	ioutil.WriteFile(tc.Paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	// Validate we got a new valid state
	newNodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.NotNil(t, nodeConfig.RootCA.Signer)
	assert.True(t, nodeConfig.RootCA.CanSign())

	// Ensure that we have the same certificate as before
	assert.Equal(t, nodeConfig.RootCA.Cert, newNodeConfig.RootCA.Cert)
}

func TestLoadOrCreateSecurityConfigInvalidCAKey(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the root key
	ioutil.WriteFile(tc.Paths.RootCA.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.Nil(t, nodeConfig.RootCA.Signer)
	assert.False(t, nodeConfig.RootCA.CanSign())
}

func TestLoadOrCreateSecurityConfigInvalidCert(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.Node.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.NotNil(t, nodeConfig.RootCA.Signer)
}

func TestLoadOrCreateSecurityConfigInvalidKey(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA.Pool)
	assert.NotNil(t, nodeConfig.RootCA.Cert)
	assert.NotNil(t, nodeConfig.RootCA.Signer)
}

func TestLoadOrCreateSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.Remove(tc.Paths.Node.Cert)
	os.Remove(tc.Paths.RootCA.Key)
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, nil)
	assert.EqualError(t, err, "valid remote address picker required")
}

func TestRenewTLSConfig(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	managerConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	var serverSuccess, clientSuccess, timeout bool
	cConfigs, sConfigs, errs := ca.RenewTLSConfig(ctx, managerConfig, tc.TempDir, tc.Picker, 500*time.Millisecond)
	for {
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case clientTLSConfig := <-cConfigs:
			assert.NotNil(t, clientTLSConfig)
			clientSuccess = true
			err := managerConfig.ClientTLSCreds.LoadNewTLSConfig(&clientTLSConfig)
			assert.NoError(t, err)
		case serverTLSConfig := <-sConfigs:
			serverSuccess = true
			assert.NotNil(t, serverTLSConfig)
			err := managerConfig.ServerTLSCreds.LoadNewTLSConfig(&serverTLSConfig)
			assert.NoError(t, err)
		case err = <-errs:
		}
		if err != nil {
			assert.Fail(t, err.Error())
			break
		}
		if timeout {
			assert.Fail(t, "TestRenewTLSConfig timed-out")
			break
		}
		if clientSuccess && serverSuccess {
			break
		}
	}
	assert.True(t, clientSuccess)
	assert.True(t, serverSuccess)
}
