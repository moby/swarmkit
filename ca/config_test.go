package ca

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/stretchr/testify/assert"
)

func TestLoadManagerSecurityConfigWithEmptyDir(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	managerConfig, err := LoadOrCreateManagerSecurityConfig(context.Background(), tempBaseDir, "", nil)
	assert.NoError(t, err)
	assert.NotNil(t, managerConfig)
	assert.NotNil(t, managerConfig.RootCA.Signer)
	assert.NotNil(t, managerConfig.RootCA.Cert)
	assert.NotNil(t, managerConfig.RootCA.Pool)
	assert.NotNil(t, managerConfig.ClientTLSCreds)
	assert.NotNil(t, managerConfig.ServerTLSCreds)
}

func TestLoadOrCreateManagerSecurityConfigNoCARemoteManager(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tc.tmpDir)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.Nil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigNoCerts(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tc.paths.Manager.Cert)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCACertNoRemote(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", nil)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCACertWithRemote(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.Nil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCert(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the cert
	ioutil.WriteFile(tc.paths.Manager.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidKey(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Write some garbage to the Key
	ioutil.WriteFile(tc.paths.Manager.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
													some random garbage\n
													-----END EC PRIVATE KEY-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.Remove(tc.paths.Manager.Cert)
	os.Remove(tc.paths.RootCA.Key)
	_, err := LoadOrCreateManagerSecurityConfig(tc.ctx, tc.tmpDir, "", nil)
	assert.EqualError(t, err, "valid remote address picker required")
}

func TestLoadOrCreateAgentSecurityConfigNoCARemoteManager(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tc.tmpDir)
	agentSecurityConfig, err := LoadOrCreateAgentSecurityConfig(tc.ctx, tc.tmpDir, "", tc.picker)
	assert.NoError(t, err)
	assert.NotNil(t, agentSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, agentSecurityConfig.Pool)
}

func TestLoadOrCreateAgentSecurityConfigNoCANoRemoteManager(t *testing.T) {
	tc := NewTestCA(t, AutoAcceptPolicy())
	defer tc.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tc.tmpDir)
	_, err := LoadOrCreateAgentSecurityConfig(tc.ctx, tc.tmpDir, "", nil)
	assert.EqualError(t, err, "valid remote address picker required")
}
