package testutils

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/cloudflare/cfssl/signer"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/identity"
)

// TestCA is struct for creating certs for managers and agents who can talk to
// each other.
type TestCA struct {
	dir        string
	signer     signer.Signer
	paths      *ca.SecurityConfigPaths
	rootCACert []byte
	rootCAPool *x509.CertPool
}

// NewTestCA returns initialized with signer, cert and pool TestCA.
func NewTestCA() (*TestCA, error) {
	dir, err := ioutil.TempDir("", "swarm-agent-test-")
	if err != nil {
		return nil, err
	}

	paths := ca.NewConfigPaths(dir)

	signer, rootCACert, err := ca.CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	if err != nil {
		return nil, err
	}

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return nil, fmt.Errorf("failed to append certificate to cert pool")
	}
	return &TestCA{
		dir:        dir,
		paths:      paths,
		signer:     signer,
		rootCACert: rootCACert,
		rootCAPool: rootCAPool,
	}, nil
}

// Close removes temp directory.
func (tca *TestCA) Close() error {
	return os.RemoveAll(tca.dir)
}

// ManagerConfig returns security config for manager usage.
func (tca *TestCA) ManagerConfig() (*ca.ManagerSecurityConfig, error) {
	managerID := identity.NewID()
	managerCert, err := ca.GenerateAndSignNewTLSCert(tca.signer, tca.rootCACert, tca.paths.ManagerCert, tca.paths.ManagerKey, managerID, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := ca.NewServerTLSCredentials(managerCert, tca.rootCAPool)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := ca.NewClientTLSCredentials(managerCert, tca.rootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.ManagerSecurityConfig{
		RootCACert:     tca.rootCACert,
		RootCAPool:     tca.rootCAPool,
		Signer:         tca.signer,
		ServerTLSCreds: managerTLSCreds,
		ClientTLSCreds: managerClientTLSCreds,
		RootCA:         true,
	}, nil
}

// AgentConfig returns securty config for agent usage.
func (tca *TestCA) AgentConfig() (*ca.AgentSecurityConfig, error) {
	agentID := identity.NewID()
	agentCert, err := ca.GenerateAndSignNewTLSCert(tca.signer, tca.rootCACert, tca.paths.AgentCert, tca.paths.AgentKey, agentID, ca.AgentRole)
	if err != nil {
		return nil, err
	}

	agentClientTLSCreds, err := ca.NewClientTLSCredentials(agentCert, tca.rootCAPool, ca.ManagerRole)
	if err != nil {
		return nil, err
	}

	return &ca.AgentSecurityConfig{
		RootCAPool:     tca.rootCAPool,
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
