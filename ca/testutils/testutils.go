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
func GenerateAgentAndManagerSecurityConfig() (*ca.AgentSecurityConfig, *ca.ManagerSecurityConfig, string, error) {
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

	agentID := identity.NewID()
	agentCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, paths.AgentCert, paths.AgentKey, agentID, ca.AgentRole)
	if err != nil {
		return nil, nil, "", err
	}

	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(signer, rootCACert, paths.ManagerCert, paths.ManagerKey, managerID, ca.ManagerRole)
	if err != nil {
		return nil, nil, "", err
	}

	agentClientTLSCreds, err := ca.NewClientTLSCredentials(agentCert, rootCAPool, ca.ManagerRole)
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

	AgentSecurityConfig := &ca.AgentSecurityConfig{}
	AgentSecurityConfig.RootCAPool = rootCAPool
	AgentSecurityConfig.ClientTLSCreds = agentClientTLSCreds

	ManagerSecurityConfig := &ca.ManagerSecurityConfig{}
	ManagerSecurityConfig.RootCACert = rootCACert
	ManagerSecurityConfig.RootCAPool = rootCAPool
	ManagerSecurityConfig.ServerTLSCreds = managerTLSCreds
	ManagerSecurityConfig.ClientTLSCreds = managerClientTLSCreds
	ManagerSecurityConfig.RootCA = true
	ManagerSecurityConfig.Signer = signer

	return AgentSecurityConfig, ManagerSecurityConfig, tempBaseDir, nil
}
