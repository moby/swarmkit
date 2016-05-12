package ca

import (
	"io/ioutil"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/manager/state/store"
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
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
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
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()
	defer caserver.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", l.Addr().String())
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.Nil(t, newManagerSecurityConfig.RootCA.Signer)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateManagerSecurityConfigNoCerts(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
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
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()
	defer caserver.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(paths.Manager.Cert)
	newManagerSecurityConfig, err := LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", l.Addr().String())
	assert.NoError(t, err)
	assert.NotNil(t, newManagerSecurityConfig)
	assert.NotNil(t, newManagerSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.ServerTLSCreds)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Pool)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Cert)
	assert.NotNil(t, newManagerSecurityConfig.RootCA.Signer)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateManagerSecurityConfigNoCertsAndNoRemote(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-manager-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
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
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()
	defer caserver.Stop()

	// Remove the certificate from the temp dir and try loading with a new manager
	os.RemoveAll(paths.Manager.Cert)
	os.RemoveAll(paths.RootCA.Key)
	_, err = LoadOrCreateManagerSecurityConfig(ctx, tempBaseDir, "", "")
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

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
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
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()
	defer caserver.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	agentSecurityConfig, err := LoadOrCreateAgentSecurityConfig(ctx, tempBaseDir, "", l.Addr().String())
	assert.NoError(t, err)
	assert.NotNil(t, agentSecurityConfig.ClientTLSCreds)
	assert.NotNil(t, agentSecurityConfig.Pool)

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func TestLoadOrCreateAgentSecurityConfigNoCANoRemoteManager(t *testing.T) {
	tempBaseDir, err := ioutil.TempDir("", "swarm-agent-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	rootCA, err := CreateAndWriteRootCA("swarm-test-CA", paths.RootCA)
	assert.NoError(t, err)
	managerConfig, err := genManagerSecurityConfig(rootCA, tempBaseDir)
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
	go func() {
		assert.NoError(t, caserver.Run(context.Background()))
	}()
	defer caserver.Stop()

	// Remove all the contents from the temp dir and try again with a new manager
	os.RemoveAll(tempBaseDir)
	_, err = LoadOrCreateAgentSecurityConfig(ctx, tempBaseDir, "", "")
	assert.Error(t, err, "address of a manager is required to join a cluster")

	grpcServer.Stop()

	// After stopping we should receive an error from ListenAndServe.
	assert.Error(t, <-done)
}

func genManagerSecurityConfig(rootCA RootCA, tempBaseDir string) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(tempBaseDir)

	managerID := identity.NewID()
	managerCert, err := GenerateAndSignNewTLSCert(rootCA, managerID, ManagerRole, paths.Manager)
	if err != nil {
		return nil, err
	}

	managerTLSCreds, err := rootCA.NewServerTLSCredentials(managerCert)
	if err != nil {
		return nil, err
	}

	managerClientTLSCreds, err := rootCA.NewClientTLSCredentials(managerCert, ManagerRole)
	if err != nil {
		return nil, err
	}

	ManagerSecurityConfig := &ManagerSecurityConfig{}
	ManagerSecurityConfig.RootCA = rootCA
	ManagerSecurityConfig.ServerTLSCreds = managerTLSCreds
	ManagerSecurityConfig.ClientTLSCreds = managerClientTLSCreds

	return ManagerSecurityConfig, nil
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
