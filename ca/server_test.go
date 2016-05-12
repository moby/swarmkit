package ca

import (
	"crypto/tls"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"github.com/stretchr/testify/assert"
)

type grpcCA struct {
	Clients    []api.CAClient
	Store      *store.MemoryStore
	grpcServer *grpc.Server
	caServer   *Server
	conns      []*grpc.ClientConn
}

func (ga *grpcCA) Close() {
	// Close the client connection.
	for _, conn := range ga.conns {
		conn.Close()
	}
	ga.grpcServer.Stop()
}

func startCA() (*grpcCA, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}

	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, "swarm-test-CA")
	if err != nil {
		return nil, err
	}

	managerSecurityConfig, err := genManagerSecurityConfig(signer, rootCACert, tempBaseDir)
	if err != nil {
		return nil, err
	}

	agentSecurityConfig, err := genAgentSecurityConfig(signer, rootCACert, tempBaseDir)
	if err != nil {
		return nil, err
	}

	serverOpts := []grpc.ServerOption{grpc.Creds(managerSecurityConfig.ServerTLSCreds)}

	s := grpc.NewServer(serverOpts...)
	store := store.NewMemoryStore(nil)
	ca := NewServer(store, managerSecurityConfig)
	api.RegisterCAServer(s, ca)
	go func() {
		// Serve will always return an error (even when properly stopped).
		// Explicitly ignore it.
		_ = s.Serve(l)
	}()

	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	insecureClientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(insecureCreds)}

	clientOpts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second)}
	clientOpts = append(clientOpts, grpc.WithTransportCredentials(agentSecurityConfig.ClientTLSCreds))

	conn1, err := grpc.Dial(l.Addr().String(), insecureClientOpts...)
	if err != nil {
		s.Stop()
		return nil, err
	}

	conn2, err := grpc.Dial(l.Addr().String(), clientOpts...)
	if err != nil {
		s.Stop()
		return nil, err
	}

	clients := []api.CAClient{api.NewCAClient(conn1), api.NewCAClient(conn2)}
	conns := []*grpc.ClientConn{conn1, conn2}
	return &grpcCA{
		Clients:    clients,
		Store:      store,
		caServer:   ca,
		conns:      conns,
		grpcServer: s,
	}, nil
}

func TestGetRootCACertificate(t *testing.T) {
	gc, err := startCA()
	assert.NoError(t, err)
	defer gc.Close()

	resp, err := gc.Clients[0].GetRootCACertificate(context.Background(), &api.GetRootCACertificateRequest{})
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Certificate)
}

func TestIssueCertificate(t *testing.T) {
	gc, err := startCA()
	assert.NoError(t, err)
	defer gc.Close()

	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	csr, _, err := GenerateAndWriteNewCSR(paths.ManagerCSR, paths.ManagerKey)
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	role := AgentRole
	issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}

	resp, err := gc.Clients[1].IssueCertificate(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.Token)

	gc.Store.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, storeCerts[0].Status.State, api.IssuanceStatePending)
	})
}

func TestCertificateStatus(t *testing.T) {
	gc, err := startCA()
	assert.NoError(t, err)
	defer gc.Close()

	tempBaseDir, err := ioutil.TempDir("", "swarm-ca-test-")
	assert.NoError(t, err)
	defer os.RemoveAll(tempBaseDir)

	paths := NewConfigPaths(tempBaseDir)

	csr, _, err := GenerateAndWriteNewCSR(paths.ManagerCSR, paths.ManagerKey)
	assert.NoError(t, err)
	assert.NotNil(t, csr)

	testRegisteredCert := &api.RegisteredCertificate{
		ID:     "token",
		CN:     "cn",
		CSR:    csr,
		Status: api.IssuanceStatus{State: api.IssuanceStatePending},
	}

	err = gc.Store.Update(func(tx store.Tx) error {
		assert.NoError(t, store.CreateRegisteredCertificate(tx, testRegisteredCert))
		return nil
	})
	assert.NoError(t, err)

	gc.caServer.reconcileCertificates(context.Background(), []*api.RegisteredCertificate{testRegisteredCert})

	issueRequest := &api.CertificateStatusRequest{Token: "token"}
	resp, err := gc.Clients[1].CertificateStatus(context.Background(), issueRequest)
	assert.NoError(t, err)
	assert.NotEmpty(t, resp.RegisteredCertificate)
	assert.NotEmpty(t, resp.RegisteredCertificate.Certificate)
	assert.NotEmpty(t, resp.Status)
	assert.Equal(t, resp.Status.State, api.IssuanceStateIssued)

	gc.Store.View(func(readTx store.ReadTx) {
		storeCerts, err := store.FindRegisteredCertificates(readTx, store.All)
		assert.NoError(t, err)
		assert.NotEmpty(t, storeCerts)
		assert.Equal(t, storeCerts[0].Status.State, api.IssuanceStateIssued)
	})
}
