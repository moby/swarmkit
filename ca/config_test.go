package ca_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/net/context"

	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/stretchr/testify/assert"
)

func TestLoadOrCreateSecurityConfigEmptyDir(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	info := make(chan string, 1)
	// Remove all the contents from the temp dir and try again with a new node
	os.RemoveAll(tc.TempDir)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, info)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.Nil(t, nodeConfig.RootCA().Signer)
	assert.False(t, nodeConfig.RootCA().CanSign())
	assert.NotEmpty(t, <-info)
}

func TestLoadOrCreateSecurityConfigNoCerts(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are locally signed
	os.RemoveAll(tc.Paths.Node.Cert)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
	assert.True(t, nodeConfig.RootCA().CanSign())

	info := make(chan string, 1)
	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are issued by the remote CA
	os.RemoveAll(tc.Paths.RootCA.Key)
	os.RemoveAll(tc.Paths.Node.Cert)
	nodeConfig, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, info)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.Nil(t, nodeConfig.RootCA().Signer)
	assert.False(t, nodeConfig.RootCA().CanSign())
	assert.NotEmpty(t, <-info)
}

func TestLoadOrCreateSecurityConfigNoLocalCACertNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Delete the root CA file so that LoadOrCreateSecurityConfig falls
	// back to using the remote.
	assert.Nil(t, os.Remove(tc.Paths.RootCA.Cert))

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, nil, nil)
	assert.EqualError(t, err, "valid remote address picker required")
	assert.Nil(t, nodeConfig)
}

func TestLoadOrCreateSecurityConfigInvalidCACert(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// First load the current nodeConfig. We'll verify that after we corrupt
	// the certificate, another subsquent call with get us new certs
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	// We have a valid signer because we bootstrapped with valid root key-material
	assert.NotNil(t, nodeConfig.RootCA().Signer)
	assert.True(t, nodeConfig.RootCA().CanSign())

	// Write some garbage to the CA cert
	ioutil.WriteFile(tc.Paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	// We should get an error when the CA cert is invalid.
	_, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.Error(t, err)

	// Not having a local cert should cause us to fallback to using the
	// picker to get a remote.
	assert.Nil(t, os.Remove(tc.Paths.RootCA.Cert))

	// Validate we got a new valid state
	newNodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
	assert.True(t, nodeConfig.RootCA().CanSign())

	// Ensure that we have the same certificate as before
	assert.Equal(t, nodeConfig.RootCA().Cert, newNodeConfig.RootCA().Cert)
}

func TestLoadOrCreateSecurityConfigInvalidCAKey(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the root key
	ioutil.WriteFile(tc.Paths.RootCA.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	// We should get an error when the local ca private key is invalid.
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.Error(t, err)
}

func TestLoadOrCreateSecurityConfigInvalidCert(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.Node.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigInvalidKey(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigInvalidKeyWithValidTempKey(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)

	// Write some garbage to the Key
	assert.NoError(t, os.Rename(tc.Paths.Node.Key, filepath.Dir(tc.Paths.Node.Key)+"."+filepath.Base(tc.Paths.Node.Key)))
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)
	nodeConfig, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.Remove(tc.Paths.Node.Cert)
	os.Remove(tc.Paths.RootCA.Key)
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", "", ca.AgentRole, nil, nil)
	assert.EqualError(t, err, "valid remote address picker required")
}

func TestRenewTLSConfig(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tc.RootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Get a new managerConfig with a TLS cert that has 15 minutes to live
	managerConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	var success, timeout bool
	renew := make(chan struct{})
	updates := ca.RenewTLSConfig(ctx, managerConfig, tc.TempDir, tc.Picker, renew)
	for {
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case certUpdate := <-updates:
			assert.NoError(t, certUpdate.Err)
			assert.NotNil(t, certUpdate)
			assert.Equal(t, certUpdate.Role, ca.ManagerRole)
			success = true
		}
		if timeout {
			assert.Fail(t, "TestRenewTLSConfig timed-out")
			break
		}
		if success {
			break
		}
	}
}

func TestForceRenewTLSConfig(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Replace the RootCA with one with a signer that signs certs with 15 minute expiration
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Key, 15*time.Minute)
	assert.NoError(t, err)
	tc.RootCA = newRootCA

	// Get a new managerConfig with a TLS cert that has 15 minutes to live
	managerConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	var success, timeout bool
	renew := make(chan struct{}, 1)
	updates := ca.RenewTLSConfig(ctx, managerConfig, tc.TempDir, tc.Picker, renew)
	for {
		renew <- struct{}{}
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case certUpdate := <-updates:
			assert.NoError(t, certUpdate.Err)
			assert.NotNil(t, certUpdate)
			assert.Equal(t, certUpdate.Role, ca.ManagerRole)
			success = true
		}
		if timeout {
			assert.Fail(t, "TestForceRenewTLSConfig timed-out")
			break
		}
		if success {
			break
		}
	}
}
