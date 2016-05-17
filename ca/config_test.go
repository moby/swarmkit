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

	managerConfig, err := LoadOrCreateManagerSecurityConfig(context.Background(), tempBaseDir, "")
	assert.NoError(t, err)
	assert.NotNil(t, managerConfig)
	assert.NotNil(t, managerConfig.RootCA.Signer)
	assert.NotNil(t, managerConfig.RootCA.Cert)
	assert.NotNil(t, managerConfig.RootCA.Pool)
	assert.NotNil(t, managerConfig.ClientTLSCreds)
	assert.NotNil(t, managerConfig.ServerTLSCreds)
}

func TestLoadOrCreateManagerSecurityConfigNoCARemoteManager(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer ts.cleanup()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.tmpDir)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.Nil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigNoCerts(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer ts.cleanup()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.paths.Manager.Cert)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCACertNoRemote(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer ts.cleanup()

	// Write some garbage to the cert
	ioutil.WriteFile(ts.paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "")
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCACertWithRemote(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	ts.cleanup()

	// Write some garbage to the cert
	ioutil.WriteFile(ts.paths.RootCA.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.Nil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidCert(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())

	defer ts.cleanup()

	// Write some garbage to the cert
	ioutil.WriteFile(ts.paths.Manager.Cert, []byte(`-----BEGIN CERTIFICATE-----\n
													some random garbage\n
													-----END CERTIFICATE-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigInvalidKey(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())

	defer ts.cleanup()
	// Write some garbage to the Key
	ioutil.WriteFile(ts.paths.Manager.Key, []byte(`-----BEGIN EC PRIVATE KEY-----\n
													some random garbage\n
													-----END EC PRIVATE KEY-----`), 0644)

	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)
}

func TestLoadOrCreateManagerSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())

	defer ts.cleanup()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.Remove(ts.paths.Manager.Cert)
	os.Remove(ts.paths.RootCA.Key)
	_, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "")
	assert.EqualError(t, err, "no remote hosts provided")
}

func TestLoadOrCreateAgentSecurityConfigNoCARemoteManager(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())

	defer ts.cleanup()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.tmpDir)
	agentSecurityConfig, err := LoadOrCreateAgentSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, agentSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, agentSecurityConfig.Pool)
}

func TestLoadOrCreateAgentSecurityConfigNoCANoRemoteManager(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer ts.cleanup()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.tmpDir)
	_, err := LoadOrCreateAgentSecurityConfig(ts.ctx, ts.tmpDir, "")
	assert.EqualError(t, err, "no remote hosts provided")
}
