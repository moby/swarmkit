package ca

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/cloudflare/cfssl/signer"
	"github.com/docker/swarm-v2/identity"
	"github.com/stretchr/testify/assert"
)

func TestLoadManagerSecurityConfig(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.NoError(t, loadedManagerSecurityConfig.validate())
	assert.NotNil(t, loadedManagerSecurityConfig.Signer)
	assert.NotNil(t, loadedManagerSecurityConfig.RootCACert)
	assert.NotNil(t, loadedManagerSecurityConfig.RootCAPool)
	assert.NotNil(t, loadedManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, loadedManagerSecurityConfig.ClientTLSCreds)
	assert.True(t, loadedManagerSecurityConfig.RootCA)
}

func TestLoadManagerSecurityConfigWithEmptyDir(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadManagerSecurityConfigWithNoCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.RootCACert)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadManagerSecurityConfigWithNoKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.RootCAKey)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.NoError(t, loadedManagerSecurityConfig.validate())
	assert.Nil(t, loadedManagerSecurityConfig.Signer)
}

func TestLoadManagerSecurityConfigWithCorruptedCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	err = ioutil.WriteFile(paths.RootCACert, []byte("INVALID DATA"), 0600)
	assert.NoError(t, err)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadManagerSecurityConfigWithCorruptedKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	err = ioutil.WriteFile(paths.RootCAKey, []byte("INVALID DATA"), 0600)
	assert.NoError(t, err)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.NoError(t, loadedManagerSecurityConfig.validate())
	assert.Nil(t, loadedManagerSecurityConfig.Signer)
}

func TestLoadManagerSecurityConfigWithNoTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.ManagerCert)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: invalid or inexistent TLS server certificates")
}

func TestLoadManagerSecurityConfigWithNoTLSKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-sssss")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.ManagerKey)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: invalid or inexistent TLS server certificates")
}

func TestLoadAgentSecurityConfig(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.NoError(t, loadedAgentSecurityConfig.validate())
	assert.NotNil(t, loadedAgentSecurityConfig.RootCAPool)
	assert.NotNil(t, loadedAgentSecurityConfig.ClientTLSCreds)
}

func TestLoadAgentSecurityConfigWithEmptyDir(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadAgentSecurityConfigWithNoCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.RootCACert)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadAgentSecurityConfigWithCorruptedCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	err = ioutil.WriteFile(paths.RootCACert, []byte("INVALID DATA"), 0600)
	assert.NoError(t, err)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: this node has no information about a Swarm Certificate Authority")
}

func TestLoadAgentSecurityConfigWithNoTLSCert(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.AgentCert)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: invalid or inexistent TLS server certificates")
}

func TestLoadAgentSecurityConfigWithNoTLSKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test")
	assert.NoError(t, err)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.AgentKey)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: invalid or inexistent TLS server certificates")
}

func genManagerSecurityConfig(signer signer.Signer, rootCACert []byte, tempBaseDir string) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return nil, fmt.Errorf("failed to append certificate to cert pool")
	}

	managerID := identity.NewID()
	managerCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, paths.ManagerCert, paths.ManagerKey, managerID, ManagerRole)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := NewServerTLSCredentials(managerCert, rootCAPool)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := NewClientTLSCredentials(managerCert, rootCAPool, ManagerRole)
	if err != nil {
		return nil, err
	}

	ManagerSecurityConfig := &ManagerSecurityConfig{}
	ManagerSecurityConfig.RootCACert = rootCACert
	ManagerSecurityConfig.RootCAPool = rootCAPool
	ManagerSecurityConfig.ServerTLSCreds = managerTLSCreds
	ManagerSecurityConfig.ClientTLSCreds = managerClientTLSCreds
	ManagerSecurityConfig.RootCA = true
	ManagerSecurityConfig.Signer = signer

	return ManagerSecurityConfig, nil
}

func genAgentSecurityConfig(signer signer.Signer, rootCACert []byte, tempBaseDir string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return nil, fmt.Errorf("failed to append certificate to cert pool")
	}

	agentID := identity.NewID()
	agentCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, paths.AgentCert, paths.AgentKey, agentID, AgentRole)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := NewClientTLSCredentials(agentCert, rootCAPool, ManagerRole)
	if err != nil {
		return nil, err
	}

	AgentSecurityConfig := &AgentSecurityConfig{}
	AgentSecurityConfig.RootCAPool = rootCAPool
	AgentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

	return AgentSecurityConfig, nil
}
