package testutils

import (
	"io/ioutil"
	"os"

	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/identity"
)

// TestCA is struct for creating certs for managers and agents who can talk to
// each other.
type TestCA struct {
	ca.RootCA

	dir   string
	paths *ca.SecurityConfigPaths
}

// NewTestCA returns initialized with RootCA, cert and pool TestCA.
func NewTestCA() (*TestCA, error) {
	dir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, err
	}

	paths := ca.NewConfigPaths(dir)

	rootCA, err := ca.CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	if err != nil {
		return nil, err
	}

	return &TestCA{
		RootCA: rootCA,
		dir:    dir,
		paths:  paths,
	}, nil
}

// Close removes temp directory.
func (tca *TestCA) Close() error {
	return os.RemoveAll(tca.dir)
}

// ManagerConfig returns security config for manager usage.
func (tca *TestCA) ManagerConfig() (*ca.ManagerSecurityConfig, error) {
	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(tca.RootCA, managerID, ca.ManagerRole, tca.paths.Manager)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := tca.RootCA.NewServerTLSCredentials(managerCert)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := tca.RootCA.NewClientTLSCredentials(managerCert, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.ManagerSecurityConfig{
		RootCA:         tca.RootCA,
		ServerTLSCreds: managerTLSCreds,
		ClientTLSCreds: managerClientTLSCreds,
	}, nil
}

// AgentConfig returns securty config for agent usage.
func (tca *TestCA) AgentConfig() (*ca.AgentSecurityConfig, error) {
	agentID := identity.NewID()
	agentCert, err := ca.GenerateAndSignNewTLSCert(tca.RootCA, agentID, ca.AgentRole, tca.paths.Agent)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := tca.RootCA.NewClientTLSCredentials(agentCert, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.AgentSecurityConfig{
		RootCA:         tca.RootCA,
		ClientTLSCreds: agentClientTLSCreds,
	}, nil
}

// GenerateAgentAndManagerSecurityConfig is a helper function that creates two valid
// SecurityConfigurations, one for an agent and one for a manager.
func GenerateAgentAndManagerSecurityConfig(numAgents int) ([]*ca.AgentSecurityConfig, *ca.ManagerSecurityConfig, string, error) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, nil, "", err
	}

	paths := ca.NewConfigPaths(tempBaseDir)

	rootCA, err := ca.CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	if err != nil {
		return nil, nil, "", err
	}

	var agentConfigs []*ca.AgentSecurityConfig
	for i := 0; i < numAgents; i++ {
		agentID := identity.NewID()
		agentCert, err := ca.GenerateAndSignNewTLSCert(rootCA, agentID, ca.AgentRole, paths.Agent)
		if err != nil {
			return nil, nil, "", err
		}

		agentClientTLSCreds, err := rootCA.NewClientTLSCredentials(agentCert, ca.ManagerRole)
		if err != nil {
			return nil, nil, "", err
		}

		agentSecurityConfig := &ca.AgentSecurityConfig{}
		agentSecurityConfig.RootCA = rootCA
		agentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

		agentConfigs = append(agentConfigs, agentSecurityConfig)
	}

	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(rootCA, managerID, ca.ManagerRole, paths.Manager)
	if err != nil {
		return nil, nil, "", err
	}

	managerTLSCreds, err := rootCA.NewServerTLSCredentials(managerCert)
	if err != nil {
		return nil, nil, "", err
	}

	managerClientTLSCreds, err := rootCA.NewClientTLSCredentials(managerCert, ca.ManagerRole)
	if err != nil {
		return nil, nil, "", err
	}

	managerSecurityConfig := &ca.ManagerSecurityConfig{}
	managerSecurityConfig.RootCA = rootCA
	managerSecurityConfig.ServerTLSCreds = managerTLSCreds
	managerSecurityConfig.ClientTLSCreds = managerClientTLSCreds

	return agentConfigs, managerSecurityConfig, tempBaseDir, nil
}
