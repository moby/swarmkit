package ca

import (
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/cloudflare/cfssl/signer"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/store"
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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.ManagerCert)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: invalid or nonexistent TLS server certificates")
}

func TestLoadManagerSecurityConfigWithNoTLSKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-sssss")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.ManagerKey)

	loadedManagerSecurityConfig := loadManagerSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedManagerSecurityConfig.validate(), "swarm-pki: invalid or nonexistent TLS server certificates")
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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

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
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.AgentCert)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: invalid or nonexistent TLS server certificates")
}

func TestLoadAgentSecurityConfigWithNoTLSKey(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	_, err = genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	os.Remove(paths.AgentKey)

	loadedAgentSecurityConfig := loadAgentSecurityConfig(tempBaseDir)
	assert.EqualError(t, loadedAgentSecurityConfig.validate(), "swarm-pki: invalid or nonexistent TLS server certificates")
}

func TestLoadOrCreateManagerSecurityConfigNoCA(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	ctx := context.Background()

	managerConfig, err := LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", "", true)
	assert.NoError(t, err)
	assert.NotNil(t, managerConfig)
	assert.NotNil(t, managerConfig.Signer)
	assert.NotNil(t, managerConfig.ClientTLSCreds)
	assert.NotNil(t, managerConfig.ServerTLSCreds)
	assert.NotNil(t, managerConfig.RootCAPool)
	assert.True(t, managerConfig.RootCA)
	assert.False(t, managerConfig.IntCA)
}

func TestLoadOrCreateManagerSecurityConfigNoCARemoteManager(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", l.Addr().String(), true)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCAPool)
	assert.Nil(t, newManagerSecurityConfig.Signer)
	assert.False(t, newManagerSecurityConfig.RootCA)
	assert.False(t, newManagerSecurityConfig.IntCA)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateManagerSecurityConfigNoCerts(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(paths.ManagerCert)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", l.Addr().String(), true)
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.Signer)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCAPool)
	assert.True(t, newManagerSecurityConfig.RootCA)
	assert.False(t, newManagerSecurityConfig.IntCA)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateManagerSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.RemoveAll(paths.ManagerCert)
	_, err = LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", "", true)
	assert.Error(t, err, "address of a manager is required to join a cluster")

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateAgentSecurityConfigNoCARemoteManager(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	agentSecurityConfig, err := LoadOrCreateAgentSecurityConfig(ctx, tempBaseDir, "", l.Addr().String())
	assert.NoError(t, err)
	assert.NotNil(t, agentSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, agentSecurityConfig.RootCAPool)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateAgentSecurityConfigNoCANoRemoteManager(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	assert.NoError(t, err)

	ctx := context.Background()

	opts := []grpc.ServerOption{grpc.Creds(managerConfig.ServerTLSCreds)}
	grpcServer := grpc.NewServer(opts...)
	store := store.NewMemoryStore(nil)
	caserver := NewServer(store, managerConfig)
	api.RegisterCAServer(grpcServer, caserver)
	l, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	done := make(chan error)
	defer close(done)
	go func() {
		done <- grpcServer.Serve(l)
	}()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	_, err = LoadOrCreateAgentSecurityConfig(ctx, tempBaseDir, "", "")
	assert.Error(t, err, "address of a manager is required to join a cluster")

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
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
