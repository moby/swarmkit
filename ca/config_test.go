package ca

import (
	"io/ioutil"
	"os"
	"testing"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/identity"
	"github.com/stretchr/testify/assert"
)

func TestLoadManagerSecurityConfigWithEmptyDir(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	managerConfig, err := LoadOrCreateManagerSecurityConfig(context.Background(), tempBaseDir, "", "")
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
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

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
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

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

func TestLoadOrCreateManagerSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.RemoveAll(ts.paths.Manager.Cert)
	os.RemoveAll(ts.paths.RootCA.Key)
	_, err := LoadOrCreateManagerSecurityConfig(ts.ctx, ts.tmpDir, "", "")
	assert.EqualError(t, err, "no manager address provided.")
}

func TestLoadOrCreateAgentSecurityConfigNoCARemoteManager(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.tmpDir)
	agentSecurityConfig, err := LoadOrCreateAgentSecurityConfig(ts.ctx, ts.tmpDir, "", ts.addr)
	assert.NoError(t, err)
	assert.NotNil(t, agentSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, agentSecurityConfig.Pool)
}

func TestLoadOrCreateAgentSecurityConfigNoCANoRemoteManager(t *testing.T) {
	ts := NewTestService(t, AutoAcceptPolicy())
	defer os.RemoveAll(ts.tmpDir)
	defer ts.caServer.Stop()
	defer ts.server.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(ts.tmpDir)
	_, err := LoadOrCreateAgentSecurityConfig(ts.ctx, ts.tmpDir, "", "")
	assert.EqualError(t, err, "address of a manager is required to join a cluster")
}

func genAgentSecurityConfig(rootCA RootCA, tempBaseDir string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	agentID := identity.NewID()
	agentCert, err := GenerateAndSignNewTLSCert(rootCA, agentID, AgentRole, paths.Agent)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := rootCA.NewClientTLSCredentials(agentCert, ManagerRole)
	if err != nil {
		return nil, err
	}

	AgentSecurityConfig := &AgentSecurityConfig{}
	AgentSecurityConfig.RootCA = rootCA
	AgentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

	return AgentSecurityConfig, nil
}
