package testutils

import (
	"io/ioutil"
	"path/filepath"

	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/identity"
	"google.golang.org/grpc/credentials"
)

// GenerateAgentAndManagerSecurityConfig is a helper function that creates two valid
// SecurityConfigurations, one for an agent and one for a manager.
func GenerateAgentAndManagerSecurityConfig() (*ca.AgentSecurityConfig, *ca.ManagerSecurityConfig, string, error) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, nil, "", err
	}

	pathToAgentTLSCert := filepath.Join(tempBaseDir, "swarm-agent.crt")
	pathToAgentTLSKey := filepath.Join(tempBaseDir, "swarm-agent.key")
	pathToManagerTLSCert := filepath.Join(tempBaseDir, "swarm-manager.crt")
	pathToManagerTLSKey := filepath.Join(tempBaseDir, "swarm-manager.key")
	pathToRootCACert := filepath.Join(tempBaseDir, "swarm-root-ca.crt")
	pathToRootCAKey := filepath.Join(tempBaseDir, "swarm-root-ca.key")

	signer, rootCACert, err := ca.CreateRootCA(pathToRootCACert, pathToRootCAKey, "swarm-test-CA")
	if err != nil {
		return nil, nil, "", err
	}

	agentID := identity.NewID()
	agentCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, pathToAgentTLSCert, pathToAgentTLSKey, agentID, "agent")
	if err != nil {
		return nil, nil, "", err
	}

	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, pathToManagerTLSCert, pathToManagerTLSKey, managerID, "manager")
	if err != nil {
		return nil, nil, "", err
	}

	clientTLSConfig, err := ca.NewClientTLSConfig(agentCert, rootCACert, "manager")
	if err != nil {
		return nil, nil, "", err
	}

	managerTLSConfig, err := ca.NewServerTLSConfig(managerCert, rootCACert)
	if err != nil {
		return nil, nil, "", err
	}

	managerClientTLSConfig, err := ca.NewClientTLSConfig(managerCert, rootCACert, "manager")
	if err != nil {
		return nil, nil, "", err
	}

	AgentSecurityConfig := &ca.AgentSecurityConfig{}
	AgentSecurityConfig.ClientCert = agentCert
	AgentSecurityConfig.RootCACert = rootCACert
	AgentSecurityConfig.ClientTLSCreds = credentials.NewTLS(clientTLSConfig)

	ManagerSecurityConfig := &ca.ManagerSecurityConfig{}
	ManagerSecurityConfig.ServerCert = managerCert
	ManagerSecurityConfig.RootCACert = rootCACert
	ManagerSecurityConfig.ServerTLSCreds = credentials.NewTLS(managerTLSConfig)
	ManagerSecurityConfig.ClientTLSCreds = credentials.NewTLS(managerClientTLSConfig)
	ManagerSecurityConfig.RootCA = true
	ManagerSecurityConfig.Signer = signer

	return AgentSecurityConfig, ManagerSecurityConfig, tempBaseDir, nil
}
