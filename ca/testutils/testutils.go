package testutils

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"

	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/identity"
)

// GenerateAgentAndManagerSecurityConfig is a helper function that creates two valid
// SecurityConfigurations, one for an agent and one for a manager.
func GenerateAgentAndManagerSecurityConfig(numAgents int) ([]*ca.AgentSecurityConfig, *ca.ManagerSecurityConfig, string, error) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, nil, "", err
	}

	paths := ca.NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := ca.CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	if err != nil {
		return nil, nil, "", err
	}

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return nil, nil, "", fmt.Errorf("failed to append certificate to cert pool")
	}

	var agentConfigs []*ca.AgentSecurityConfig
	for i := 0; i < numAgents; i++ {
		agentID := identity.NewID()
		agentCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, paths.AgentCert, paths.AgentKey, agentID, ca.AgentRole)
		if err != nil {
			return nil, nil, "", err
		}

		agentClientTLSCreds, err := ca.NewClientTLSCredentials(agentCert, rootCAPool, ca.ManagerRole)
		if err != nil {
			return nil, nil, "", err
		}

		agentSecurityConfig := &ca.AgentSecurityConfig{}
		agentSecurityConfig.RootCAPool = rootCAPool
		agentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

		agentConfigs = append(agentConfigs, agentSecurityConfig)
	}

	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, paths.ManagerCert, paths.ManagerKey, managerID, ca.ManagerRole)
	if err != nil {
		return nil, nil, "", err
	}

	managerTLSCreds, err := ca.NewServerTLSCredentials(managerCert, rootCAPool)
	if err != nil {
		return nil, nil, "", err
	}

	managerClientTLSCreds, err := ca.NewClientTLSCredentials(managerCert, rootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, nil, "", err
	}

	managerSecurityConfig := &ca.ManagerSecurityConfig{}
	managerSecurityConfig.RootCACert = rootCACert
	managerSecurityConfig.RootCAPool = rootCAPool
	managerSecurityConfig.ServerTLSCreds = managerTLSCreds
	managerSecurityConfig.ClientTLSCreds = managerClientTLSCreds
	managerSecurityConfig.RootCA = true
	managerSecurityConfig.Signer = signer

	return agentConfigs, managerSecurityConfig, tempBaseDir, nil
}
