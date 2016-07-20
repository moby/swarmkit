package ca_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/context"

	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/ioutils"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/stretchr/testify/assert"
)

func TestLoadOrCreateSecurityConfigEmptyDir(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	info := make(chan api.IssueNodeCertificateResponse, 1)
	// Remove all the contents from the temp dir and try again with a new node
	os.RemoveAll(tc.TempDir)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, tc.WorkerToken, ca.AgentRole, tc.Picker, info)
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
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are locally signed
	os.RemoveAll(tc.Paths.Node.Cert)
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, tc.WorkerToken, ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
	assert.True(t, nodeConfig.RootCA().CanSign())

	info := make(chan api.IssueNodeCertificateResponse, 1)
	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are issued by the remote CA
	os.RemoveAll(tc.Paths.RootCA.Key)
	os.RemoveAll(tc.Paths.Node.Cert)
	nodeConfig, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, tc.WorkerToken, ca.AgentRole, tc.Picker, info)
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

func TestLoadOrCreateSecurityConfigWrongCAHash(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	splitToken := strings.Split(tc.ManagerToken, "-")
	splitToken[2] = "1kxftv4ofnc6mt30lmgipg6ngf9luhwqopfk1tz6bdmnkubg0e"
	replacementToken := strings.Join(splitToken, "-")

	info := make(chan api.IssueNodeCertificateResponse, 1)
	// Remove only the node certificates form the directory, and attest that we get
	// new certificates that are issued by the remote CA
	os.RemoveAll(tc.Paths.RootCA.Key)
	os.RemoveAll(tc.Paths.RootCA.Cert)
	os.RemoveAll(tc.Paths.Node.Cert)
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, replacementToken, ca.AgentRole, tc.Picker, info)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "remote CA does not match fingerprint.")
}

func TestLoadOrCreateSecurityConfigNoLocalCACertNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Delete the root CA file so that LoadOrCreateSecurityConfig falls
	// back to using the remote.
	assert.Nil(t, os.Remove(tc.Paths.RootCA.Cert))

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, nil, nil)
	assert.EqualError(t, err, "valid remote address picker required")
	assert.Nil(t, nodeConfig)
}

func TestLoadOrCreateSecurityConfigInvalidCACert(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// First load the current nodeConfig. We'll verify that after we corrupt
	// the certificate, another subsquent call with get us new certs
	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
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
	_, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
	assert.Error(t, err)

	// Not having a local cert should cause us to fallback to using the
	// picker to get a remote.
	assert.Nil(t, os.Remove(tc.Paths.RootCA.Cert))

	// Validate we got a new valid state
	newNodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
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
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the root key
	ioutil.WriteFile(tc.Paths.RootCA.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	// We should get an error when the local ca private key is invalid.
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
	assert.Error(t, err)
}

func TestLoadOrCreateSecurityConfigInvalidCert(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.Paths.Node.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
some random garbage\n
-----END CERTIFICATE-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigInvalidKey(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.Paths.Node.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
some random garbage\n
-----END EC PRIVATE KEY-----`), 0644)

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigInvalidKeyWithValidTempKey(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	nodeConfig, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, tc.Picker, nil)
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
	nodeConfig, err = ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, "", ca.AgentRole, nil, nil)
	assert.NoError(t, err)
	assert.NotNil(t, nodeConfig)
	assert.NotNil(t, nodeConfig.ClientTLSCreds)
	assert.NotNil(t, nodeConfig.ServerTLSCreds)
	assert.NotNil(t, nodeConfig.RootCA().Pool)
	assert.NotNil(t, nodeConfig.RootCA().Cert)
	assert.NotNil(t, nodeConfig.RootCA().Signer)
}

func TestLoadOrCreateSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.Remove(tc.Paths.Node.Cert)
	os.Remove(tc.Paths.RootCA.Key)
	_, err := ca.LoadOrCreateSecurityConfig(tc.Context, tc.TempDir, tc.WorkerToken, ca.AgentRole, nil, nil)
	assert.EqualError(t, err, "valid remote address picker required")
}

func TestRenewTLSConfigAgent(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.AgentRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 6 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	var success, timeout bool
	renew := make(chan struct{})
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.TempDir, tc.Picker, renew)
	for {
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case certUpdate := <-updates:
			assert.NoError(t, certUpdate.Err)
			assert.NotNil(t, certUpdate)
			assert.Equal(t, ca.AgentRole, certUpdate.Role)
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

func TestRenewTLSConfigManager(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 6 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	// Get a new nodeConfig with a TLS cert that has 6 minutes to live
	var success, timeout bool
	renew := make(chan struct{})

	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.TempDir, tc.Picker, renew)
	for {
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case certUpdate := <-updates:
			assert.NoError(t, certUpdate.Err)
			assert.NotNil(t, certUpdate)
			assert.Equal(t, ca.ManagerRole, certUpdate.Role)
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

func TestRenewTLSConfigWithNoNode(t *testing.T) {
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new nodeConfig with a TLS cert that has the default Cert duration
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	// Create a new RootCA, and change the policy to issue 6 minute certificates
	newRootCA, err := ca.NewRootCA(tc.RootCA.Cert, tc.RootCA.Key, ca.DefaultNodeCertExpiration)
	assert.NoError(t, err)
	newRootCA.Signer.SetPolicy(&cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage:  []string{"signing", "key encipherment", "server auth", "client auth"},
			Expiry: 6 * time.Minute,
		},
	})

	// Create a new CSR and overwrite the key on disk
	csr, _, err := ca.GenerateAndWriteNewKey(tc.Paths.Node)
	assert.NoError(t, err)

	// Issue a new certificate with the same details as the current config, but with 6 min expiration time
	c := nodeConfig.ClientTLSCreds
	signedCert, err := newRootCA.ParseValidateAndSignCSR(csr, c.NodeID(), c.Role(), c.Organization())
	assert.NoError(t, err)
	assert.NotNil(t, signedCert)

	// Overwrite the certificate on disk with one that expires in 1 minute
	err = ioutils.AtomicWriteFile(tc.Paths.Node.Cert, signedCert, 0644)
	assert.NoError(t, err)

	// Delete the node from the backend store
	err = tc.MemoryStore.Update(func(tx store.Tx) error {
		node := store.GetNode(tx, nodeConfig.ClientTLSCreds.NodeID())
		assert.NotNil(t, node)
		return store.DeleteNode(tx, nodeConfig.ClientTLSCreds.NodeID())
	})
	assert.NoError(t, err)

	var success, timeout bool
	renew := make(chan struct{})
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.TempDir, tc.Picker, renew)
	for {
		select {
		case <-time.After(2 * time.Second):
			timeout = true
		case certUpdate := <-updates:
			assert.Error(t, certUpdate.Err)
			assert.Contains(t, certUpdate.Err.Error(), "not found when attempting to renew certificate")
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
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get a new managerConfig with a TLS cert that has 15 minutes to live
	nodeConfig, err := tc.WriteNewNodeConfig(ca.ManagerRole)
	assert.NoError(t, err)

	var success, timeout bool
	renew := make(chan struct{}, 1)
	updates := ca.RenewTLSConfig(ctx, nodeConfig, tc.TempDir, tc.Picker, renew)
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
