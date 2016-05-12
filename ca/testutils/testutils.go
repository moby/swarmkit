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
	ca.Signer

	dir   string
	paths *ca.SecurityConfigPaths
}

// NewTestCA returns initialized with signer, cert and pool TestCA.
func NewTestCA() (*TestCA, error) {
	dir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, err
	}

	paths := ca.NewConfigPaths(dir)

	signer, err := ca.CreateRootCA("swarm-test-CA", paths.RootCA)
	if err != nil {
		return nil, err
	}

	return &TestCA{
		Signer: signer,
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
	managerCert, err := ca.GenerateAndSignNewTLSCert(tca.Signer, managerID, ca.ManagerRole, tca.paths.Manager)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := ca.NewServerTLSCredentials(managerCert, tca.Signer.RootCAPool)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := ca.NewClientTLSCredentials(managerCert, tca.Signer.RootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.ManagerSecurityConfig{
		Signer:         tca.Signer,
		ServerTLSCreds: managerTLSCreds,
		ClientTLSCreds: managerClientTLSCreds,
	}, nil
}

// AgentConfig returns securty config for agent usage.
func (tca *TestCA) AgentConfig() (*ca.AgentSecurityConfig, error) {
	agentID := identity.NewID()
	agentCert, err := ca.GenerateAndSignNewTLSCert(tca.Signer, agentID, ca.AgentRole, tca.paths.Agent)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := ca.NewClientTLSCredentials(agentCert, tca.Signer.RootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.AgentSecurityConfig{
		RootCAPool:     tca.Signer.RootCAPool,
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

	signer, err := ca.CreateRootCA("swarm-test-CA", paths.RootCA)
	if err != nil {
		return nil, nil, "", err
	}

	var agentConfigs []*ca.AgentSecurityConfig
	for i := 0; i < numAgents; i++ {
		agentID := identity.NewID()
		agentCert, err := ca.GenerateAndSignNewTLSCert(signer, agentID, ca.AgentRole, paths.Agent)
		if err != nil {
			return nil, nil, "", err
		}

		agentClientTLSCreds, err := ca.NewClientTLSCredentials(agentCert, signer.RootCAPool, ca.ManagerRole)
		if err != nil {
			return nil, nil, "", err
		}

		agentSecurityConfig := &ca.AgentSecurityConfig{}
		agentSecurityConfig.RootCAPool = signer.RootCAPool
		agentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

		agentConfigs = append(agentConfigs, agentSecurityConfig)
	}

	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(signer, managerID, ca.ManagerRole, paths.Manager)
	if err != nil {
		return nil, nil, "", err
	}

	managerTLSCreds, err := ca.NewServerTLSCredentials(managerCert, signer.RootCAPool)
	if err != nil {
		return nil, nil, "", err
	}

	managerClientTLSCreds, err := ca.NewClientTLSCredentials(managerCert, signer.RootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, nil, "", err
	}

	managerSecurityConfig := &ca.ManagerSecurityConfig{}
	managerSecurityConfig.Signer = signer
	managerSecurityConfig.ServerTLSCreds = managerTLSCreds
	managerSecurityConfig.ClientTLSCreds = managerClientTLSCreds

	return agentConfigs, managerSecurityConfig, tempBaseDir, nil
}
