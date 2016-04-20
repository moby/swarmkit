package ca

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"golang.org/x/net/context"
	"google.golang.org/grpc"

	"google.golang.org/grpc/credentials"
)

const (
	rootCACertFilename     = "swarm-root-ca.crt"
	rootCAKeyFilename      = "swarm-root-ca.key"
	intCACertFilename      = "swarm-intermediate-ca.crt"
	intCAKeyFilename       = "swarm-intermediate-ca.key"
	managerTLSCertFilename = "swarm-manager.crt"
	managerTLSKeyFilename  = "swarm-manager.key"
	managerCSRFilename     = "swarm-manager.csr"
	agentTLSCertFilename   = "swarm-agent.crt"
	agentTLSKeyFilename    = "swarm-agent.key"
	agentCSRFilename       = "swarm-agent.csr"
	// TODO(diogo): These names should be randomly generated, and probably used as filenames to allow agents
	// to participate in multiple clusters without filename collision
	rootCN = "swarm-ca"
	// ManagerRole represents the Manager node type, and is used for authorization to endpoints
	ManagerRole = "manager"
	// AgentRole represents the Agent node type, and is used for authorization to endpoints
	AgentRole = "agent"
)

// InvalidTLSCertificatesError represents a failure validating the TLS Certificates
type InvalidTLSCertificatesError struct{}

func (err InvalidTLSCertificatesError) Error() string {
	return fmt.Sprintf("swarm-pki: invalid or inexistent TLS server certificates")
}

// NoSwarmCAError represents a failure when retreiving an existing Certificate Authority
type NoSwarmCAError struct{}

func (err NoSwarmCAError) Error() string {
	return fmt.Sprintf("swarm-pki: this node has no information about a Swarm Certificate Authority")
}

// AgentSecurityConfig is used to configure the security params of the agents
type AgentSecurityConfig struct {
	RootCAPool     *x509.CertPool
	ClientTLSCreds credentials.TransportAuthenticator
}

func (s *AgentSecurityConfig) validate() error {
	// TODO(diogo): Validate expiration of all certificates
	// Having a valid Root CA Pool is mandatory
	if s.RootCAPool == nil {
		return NoSwarmCAError{}
	}

	// We also need valid Server TLS Certificates to join the cluster
	if s.ClientTLSCreds == nil {
		return InvalidTLSCertificatesError{}
	}

	return nil
}

// ManagerSecurityConfig is used to configure the CA stack of the manager
type ManagerSecurityConfig struct {
	RootCACert     []byte
	RootCAPool     *x509.CertPool
	RootCA         bool
	IntCA          bool
	ServerTLSCreds credentials.TransportAuthenticator
	ClientTLSCreds credentials.TransportAuthenticator
	Signer         signer.Signer
}

func (s *ManagerSecurityConfig) validate() error {
	// TODO(diogo): Validate expiration of all certificates
	// Having a Root CA certificate and Pool are mandatory
	if s.RootCACert == nil || s.RootCAPool == nil {
		return NoSwarmCAError{}
	}

	// We also need valid Server TLS Certificates
	if s.ServerTLSCreds == nil || s.ClientTLSCreds == nil {
		return InvalidTLSCertificatesError{}
	}

	return nil
}

// DefaultPolicy is the default policy used by the signers to ensure that the only fields
// from the remote CSRs we trust are: PublicKey, PublicKeyAlgorithm and SignatureAlgorithm.
var DefaultPolicy = func() *cfconfig.Signing {
	return &cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage: []string{"signing", "key encipherment", "server auth", "client auth"},
			// 3 months of expiry
			ExpiryString: "2160h",
			Expiry:       2160 * time.Hour,
			// Only trust the key components from the CSR. Everything else should
			// come directly from API call params.
			CSRWhitelist: &cfconfig.CSRWhitelist{
				PublicKey:          true,
				PublicKeyAlgorithm: true,
				SignatureAlgorithm: true,
			},
		},
	}
}

// SecurityConfigPaths is used as a helper to hold all the pashs of security relevant files
type SecurityConfigPaths struct {
	AgentCert, AgentKey, AgentCSR       string
	ManagerCert, ManagerKey, ManagerCSR string
	RootCACert, RootCAKey               string
	IntCACert, IntCAKey                 string
}

// NewConfigPaths returns the absolute paths to all of the different types of files
func NewConfigPaths(baseCertDir string) *SecurityConfigPaths {
	pathToAgentTLSCert := filepath.Join(baseCertDir, agentTLSCertFilename)
	pathToAgentTLSKey := filepath.Join(baseCertDir, agentTLSKeyFilename)
	pathToAgentCSR := filepath.Join(baseCertDir, agentCSRFilename)
	pathToManagerTLSCert := filepath.Join(baseCertDir, managerTLSCertFilename)
	pathToManagerTLSKey := filepath.Join(baseCertDir, managerTLSKeyFilename)
	pathToManagerCSR := filepath.Join(baseCertDir, managerCSRFilename)
	pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
	pathToRootCAKey := filepath.Join(baseCertDir, rootCAKeyFilename)
	pathToIntCACert := filepath.Join(baseCertDir, intCACertFilename)
	pathToIntCAKey := filepath.Join(baseCertDir, intCAKeyFilename)

	return &SecurityConfigPaths{
		AgentCert:   pathToAgentTLSCert,
		AgentKey:    pathToAgentTLSKey,
		AgentCSR:    pathToAgentCSR,
		ManagerCert: pathToManagerTLSCert,
		ManagerKey:  pathToManagerTLSKey,
		ManagerCSR:  pathToManagerCSR,
		RootCACert:  pathToRootCACert,
		RootCAKey:   pathToRootCAKey,
		IntCACert:   pathToIntCACert,
		IntCAKey:    pathToIntCAKey,
	}
}

func loadAgentSecurityConfig(baseCertDir string) *AgentSecurityConfig {
	paths := NewConfigPaths(baseCertDir)

	securityConfig := &AgentSecurityConfig{}
	// Check if we have joined any cluster before and store the CA's certificate PEM file
	rootCACert, err := GetRootCA(paths.RootCACert)
	if err == nil {
		// Create a Pool with our RootCACertificate
		rootCAPool := x509.NewCertPool()
		if rootCAPool.AppendCertsFromPEM(rootCACert) {
			securityConfig.RootCAPool = rootCAPool
		}
	}

	agentTLSCert, err := tls.LoadX509KeyPair(paths.AgentCert, paths.AgentKey)
	if err == nil {
		creds, err := NewClientTLSCredentials(&agentTLSCert, securityConfig.RootCAPool, ManagerRole)
		if err == nil {
			securityConfig.ClientTLSCreds = creds
		}
	}

	return securityConfig
}

func loadManagerSecurityConfig(baseCertDir string) *ManagerSecurityConfig {
	paths := NewConfigPaths(baseCertDir)

	securityConfig := &ManagerSecurityConfig{}
	// Check if we have joined any cluster before and store the CA's certificate PEM file
	rootCACert, err := GetRootCA(paths.RootCACert)
	if err == nil {
		securityConfig.RootCACert = rootCACert
		// Create a Pool with our RootCACertificate
		rootCAPool := x509.NewCertPool()
		if rootCAPool.AppendCertsFromPEM(rootCACert) {
			securityConfig.RootCAPool = rootCAPool
		}
	}

	// If there is a root CA full keypair, we're going to get a signer from it and set rootCA to true
	securityConfig.Signer, err = local.NewSignerFromFile(paths.RootCACert, paths.RootCAKey, DefaultPolicy())
	if err == nil {
		log.Debugf("loaded a root CA from: %s\n", paths.RootCACert)
		securityConfig.RootCA = true
	}

	// If there was no root CA but there is an intermediate CA, we're going to get a signer from it
	// and set intermediateCA to true
	if securityConfig.Signer == nil {
		securityConfig.Signer, err = local.NewSignerFromFile(paths.IntCACert, paths.IntCAKey, DefaultPolicy())
		if err == nil {
			log.Debugf("loaded an intermediate CA from: %s\n", paths.IntCACert)
			securityConfig.IntCA = true
		}
	}

	serverCert, err := tls.LoadX509KeyPair(paths.ManagerCert, paths.ManagerKey)
	if err == nil {
		log.Debugf("loaded server TLS certificate from: %s\n", paths.ManagerCert)
		serverTLSCreds, err := NewServerTLSCredentials(&serverCert, securityConfig.RootCAPool)
		if err == nil {
			securityConfig.ServerTLSCreds = serverTLSCreds
		}
		clientTLSCreds, err := NewClientTLSCredentials(&serverCert, securityConfig.RootCAPool, ManagerRole)
		if err == nil {
			securityConfig.ClientTLSCreds = clientTLSCreds
		}
	}

	return securityConfig
}

// LoadOrCreateAgentSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as an Agent. Every agent requires at least a set of TLS certificates with which to join
// the cluster.
func LoadOrCreateAgentSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	// Attempts to load and validate the current state on disk.
	securityConfig := loadAgentSecurityConfig(baseCertDir)
	err := securityConfig.validate()
	if err == nil {
		return securityConfig, nil
	}
	log.Debugf("failed to validate the agent config: %v\n", err)

	// As an agent we need a manager to be able to join the cluster
	if managerAddr == "" {
		return nil, fmt.Errorf("address of a manager is required to join a cluster")
	}

	// Make sure the necessary dirs exist and they are writable
	err = os.MkdirAll(baseCertDir, 0755)
	if err != nil {
		return nil, err
	}

	// If the RootCACert is nil, we should retrieve the CA from the remote host
	// checking to see if the signature matches the hash provided by the user.
	if securityConfig.RootCAPool == nil {
		rootCACert, err := GetRemoteCA(managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// Create a Pool with our RootCACertificate
		rootCAPool := x509.NewCertPool()
		if !rootCAPool.AppendCertsFromPEM(rootCACert) {
			return nil, fmt.Errorf("failed to append certificate to cert pool")
		}

		// If creating a pool succeeded, save the rootCA to disk
		if err := ioutil.WriteFile(paths.RootCACert, rootCACert, 0644); err != nil {
			return nil, err
		}

		securityConfig.RootCAPool = rootCAPool
	}

	// Create a new key/pair and CSR
	csr, _, err := GenerateAndWriteNewCSR(paths.AgentCSR, paths.AgentKey)
	if err != nil {
		return nil, err
	}

	// Get a certificate issued by this CA
	signedCert, err := getSignedCertificate(ctx, csr, securityConfig.RootCAPool, AgentRole, managerAddr)
	if err != nil {
		return nil, err
	}

	// Write the chain to disk
	if err := ioutil.WriteFile(paths.AgentCert, signedCert, 0644); err != nil {
		return nil, err
	}

	// Load the agent key pair into a tls.Certificate
	tlsKeyPair, err := tls.LoadX509KeyPair(paths.AgentCert, paths.AgentKey)
	if err != nil {
		return nil, err
	}

	// Create a client TLSConfig out of the key pair
	clientTLSCreds, err := NewClientTLSCredentials(&tlsKeyPair, securityConfig.RootCAPool, ManagerRole)
	if err != nil {
		return nil, err
	}

	securityConfig.ClientTLSCreds = clientTLSCreds

	return securityConfig, nil
}

// LoadOrCreateManagerSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as a Manager. Every manager requires at least a set of TLS certificates with which to serve
// the cluster and the dispatcher's server. Additionally, passing the CA param as true means that
// this manager will also be taking intermediate CA responsabilities, and should ensure to request
// an intermediate certificate. Finally, if no manager addresses are provided, we assume we're
// creating a new Cluster, and this manager will have to generate its own self-signed CA.
func LoadOrCreateManagerSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string, CA bool) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	// Attempt to load the current files available on this baseDir
	securityConfig := loadManagerSecurityConfig(baseCertDir)
	err := securityConfig.validate()
	if err == nil {
		return securityConfig, nil
	}
	log.Debugf("failed to validate the manager config: %v\n", err)

	// Make sure the necessary dirs exist and they are writable
	derr := os.MkdirAll(baseCertDir, 0755)
	if derr != nil {
		return nil, derr
	}

	switch err.(type) {
	case NoSwarmCAError:
		// We have no CA and no remote managers are being passed in, means we're creating a new cluster
		if managerAddr == "" {
			// Create our new RootCA and write the certificate and key to disk
			signer, rootCACert, err := CreateRootCA(paths.RootCACert, paths.RootCAKey, rootCN)
			if err != nil {
				return nil, err
			}

			// Create a Pool with our RootCACertificate
			rootCAPool := x509.NewCertPool()
			if !rootCAPool.AppendCertsFromPEM(rootCACert) {
				return nil, fmt.Errorf("failed to append certificate to cert pool")
			}

			// Create a new random ID for this manager
			managerID := identity.NewID()

			// Generate TLS Certificates using the local Root
			serverCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, paths.ManagerCert, paths.ManagerKey, managerID, ManagerRole)
			if err != nil {
				return nil, err
			}

			// Create the TLS Credentials for this manager
			serverTLSCreds, err := NewServerTLSCredentials(serverCert, rootCAPool)
			if err != nil {
				return nil, err
			}

			// Create the TLS Credentials to be used when this manager connects as a client to another remote manager
			clientTLSCreds, err := NewClientTLSCredentials(serverCert, rootCAPool, ManagerRole)
			if err != nil {
				return nil, err
			}

			// Change our Security Config to reflect our current state
			securityConfig.Signer = signer
			securityConfig.RootCA = true
			securityConfig.RootCACert = rootCACert
			securityConfig.RootCAPool = rootCAPool
			securityConfig.ServerTLSCreds = serverTLSCreds
			securityConfig.ClientTLSCreds = clientTLSCreds

			return securityConfig, nil
		}
	}

	// If we are here, we are not generating our own cluster, which means we need at least
	// one address to join
	if managerAddr == "" {
		return nil, fmt.Errorf("address of a manager is required to join a cluster")
	}

	// If the RootCACert is nil, we should retrieve the CA from the remote host
	// checking to see if the signature matches the hash provided
	if securityConfig.RootCACert == nil || securityConfig.RootCAPool == nil {
		rootCACert, err := GetRemoteCA(managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// Create a Pool with our RootCACertificate
		rootCACertPool := x509.NewCertPool()
		if !rootCACertPool.AppendCertsFromPEM(rootCACert) {
			return nil, fmt.Errorf("failed to append certificate to cert pool")
		}

		// If the root certificate got loaded successfully, save the rootCA to disk.
		if err := ioutil.WriteFile(paths.RootCACert, rootCACert, 0644); err != nil {
			return nil, err
		}

		securityConfig.RootCAPool = rootCACertPool
		securityConfig.RootCACert = rootCACert
	}

	// Create a new key/pair and CSR for the new manager
	csr, key, err := GenerateAndWriteNewCSR(paths.ManagerCSR, paths.ManagerKey)
	if err != nil {
		log.Debugf("error when generating new node certs: %v\n", err)
		return nil, err
	}

	// Get the remote manager to issue a CA signed certificate for this node
	signedCert, err := getSignedCertificate(ctx, csr, securityConfig.RootCAPool, ManagerRole, managerAddr)
	if err != nil {
		return nil, err
	}

	if CA {
		// TODO(diogo): If CA is provided we need to go get ourselves an intermediate
	}

	// Create a valid TLSKeyPair out of the PEM encoded private key and certificate
	tlsKeyPair, err := tls.X509KeyPair(signedCert, key)
	if err != nil {
		return nil, err
	}

	// Create the TLS Credentials for this manager
	securityConfig.ServerTLSCreds, err = NewServerTLSCredentials(&tlsKeyPair, securityConfig.RootCAPool)
	if err != nil {
		return nil, err
	}

	// Create a TLSConfig to be used when this manager connects as a client to another remote manager
	securityConfig.ClientTLSCreds, err = NewClientTLSCredentials(&tlsKeyPair, securityConfig.RootCAPool, ManagerRole)
	if err != nil {
		return nil, err
	}

	return securityConfig, nil
}

// newServerTLSConfig returns a tls.Config configured for a TLS Server, given a tls.Certificate
// and the PEM-encoded root CA Certificate
func newServerTLSConfig(cert *tls.Certificate, rootCAPool *x509.CertPool) (*tls.Config, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}

	return &tls.Config{
		Certificates: []tls.Certificate{*cert},
		// Since we're using the same CA server to issue Certificates to new nodes, we can't
		// use tls.RequireAndVerifyClientCert
		ClientAuth:               tls.VerifyClientCertIfGiven,
		RootCAs:                  rootCAPool,
		ClientCAs:                rootCAPool,
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}, nil
}

// newClientTLSConfig returns a tls.Config configured for a TLS Client, given a tls.Certificate
// the PEM-encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func newClientTLSConfig(cert *tls.Certificate, rootCAPool *x509.CertPool, serverName string) (*tls.Config, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}

	return &tls.Config{
		ServerName:   serverName,
		Certificates: []tls.Certificate{*cert},
		RootCAs:      rootCAPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// NewClientTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func NewClientTLSCredentials(cert *tls.Certificate, rootCAPool *x509.CertPool, serverName string) (credentials.TransportAuthenticator, error) {
	tlsConfig, err := newClientTLSConfig(cert, rootCAPool, serverName)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(tlsConfig), nil
}

// NewServerTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func NewServerTLSCredentials(cert *tls.Certificate, rootCAPool *x509.CertPool) (credentials.TransportAuthenticator, error) {
	tlsConfig, err := newServerTLSConfig(cert, rootCAPool)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(tlsConfig), nil
}

func getSignedCertificate(ctx context.Context, csr []byte, rootCAPool *x509.CertPool, role, managerAddr string) ([]byte, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}

	// This is our only non-MTLS request
	creds := credentials.NewTLS(&tls.Config{ServerName: ManagerRole, RootCAs: rootCAPool})
	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds)}

	// TODO(diogo): Add a connection picker
	conn, err := grpc.Dial(managerAddr, opts...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Create a CAClient to retreive a new Certificate
	caClient := api.NewCAClient(conn)

	// Send the Request and retrieve the certificate
	request := &api.IssueCertificateRequest{CSR: csr, Role: role}
	response, err := caClient.IssueCertificate(ctx, request)
	if err != nil {
		return nil, err
	}

	return response.CertificateChain, nil
}
