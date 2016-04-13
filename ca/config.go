package ca

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
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
	RootCACert     []byte
	ClientCert     *tls.Certificate
	ClientTLSCreds credentials.TransportAuthenticator
}

func (s *AgentSecurityConfig) validate() error {
	// TODO(diogo): Validate expiration of all certificates
	// Having a Root CA certificate is mandatory
	if s.RootCACert == nil {
		return NoSwarmCAError{}
	}

	// We also need valid Server TLS Certificates to join the cluster
	if s.ClientCert == nil {
		return InvalidTLSCertificatesError{}
	}

	return nil
}

// ManagerSecurityConfig is used to configure the CA stack of the manager
type ManagerSecurityConfig struct {
	RootCACert     []byte
	RootCA         bool
	IntCA          bool
	ServerCert     *tls.Certificate
	ServerTLSCreds credentials.TransportAuthenticator
	ClientTLSCreds credentials.TransportAuthenticator
	Signer         signer.Signer
}

func (s *ManagerSecurityConfig) validate() error {
	// TODO(diogo): Validate expiration of all certificates
	// Having a Root CA certificate is mandatory
	if s.RootCACert == nil {
		return NoSwarmCAError{}
	}

	// We also need valid Server TLS Certificates to join the cluster
	if s.ServerCert == nil {
		return InvalidTLSCertificatesError{}
	}

	return nil
}

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

func loadAgentSecurityConfig(baseCertDir string) *AgentSecurityConfig {
	pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
	pathToTLSKey := filepath.Join(baseCertDir, agentTLSKeyFilename)
	pathToTLSCertificate := filepath.Join(baseCertDir, agentTLSCertFilename)

	securityConfig := &AgentSecurityConfig{}
	// Check if we have joined any cluster before and store the CA's certificate PEM file
	rootCACert, err := GetRootCA(pathToRootCACert)
	if err == nil {
		securityConfig.RootCACert = rootCACert
	}

	agentTLSCert, err := tls.LoadX509KeyPair(pathToTLSCertificate, pathToTLSKey)
	if err == nil {
		securityConfig.ClientCert = &agentTLSCert
		tlsConfig, err := NewClientTLSConfig(&agentTLSCert, rootCACert, "manager")
		if err == nil {
			securityConfig.ClientTLSCreds = credentials.NewTLS(tlsConfig)
		}
	}

	return securityConfig
}

func loadManagerSecurityConfig(baseCertDir string) *ManagerSecurityConfig {
	pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
	pathToRootCAKey := filepath.Join(baseCertDir, rootCAKeyFilename)

	securityConfig := &ManagerSecurityConfig{}
	// Check if we have joined any cluster before and store the CA's certificate PEM file
	rootCACert, err := GetRootCA(pathToRootCACert)
	if err == nil {
		securityConfig.RootCACert = rootCACert
	}

	// If there is a root CA full keypair, we're going to get a signer from it and set rootCA to true
	securityConfig.Signer, err = local.NewSignerFromFile(pathToRootCACert, pathToRootCAKey, DefaultPolicy())
	if err == nil {
		log.Debugf("loaded a root CA from: %s\n", pathToRootCACert)
		securityConfig.RootCA = true
	}

	// If there was no root CA but there is an intermediate CA, we're going to get a signer from it
	// and set intermediateCA to true
	pathToIntermediateCACert := filepath.Join(baseCertDir, intCACertFilename)
	pathToIntermediateCAKey := filepath.Join(baseCertDir, intCAKeyFilename)
	if securityConfig.Signer == nil {
		securityConfig.Signer, err = local.NewSignerFromFile(pathToIntermediateCACert, pathToIntermediateCAKey, DefaultPolicy())
		if err == nil {
			log.Debugf("loaded an intermediate CA from: %s\n", pathToIntermediateCACert)
			securityConfig.IntCA = true
		}
	}

	pathToTLSCert := filepath.Join(baseCertDir, managerTLSCertFilename)
	pathToTLSKey := filepath.Join(baseCertDir, managerTLSKeyFilename)
	serverCert, err := tls.LoadX509KeyPair(pathToTLSCert, pathToTLSKey)
	if err == nil {
		log.Debugf("loaded server TLS certificate from: %s\n", pathToTLSCert)
		securityConfig.ServerCert = &serverCert
		serverTLSConfig, err := NewServerTLSConfig(securityConfig.ServerCert, rootCACert)
		if err == nil {
			securityConfig.ServerTLSCreds = credentials.NewTLS(serverTLSConfig)
		}
		clientTLSConfig, err := NewClientTLSConfig(&serverCert, rootCACert, "manager")
		if err == nil {
			securityConfig.ClientTLSCreds = credentials.NewTLS(clientTLSConfig)
		}

	}

	return securityConfig
}

// LoadOrCreateAgentSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as an Agent. Every agent requires at least a set of TLS certificates with which to join
// the cluster.
func LoadOrCreateAgentSecurityConfig(baseCertDir, caHash, managerAddr string) (*AgentSecurityConfig, error) {
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

	// If the RootCACert is nil, we should retrieve the CA from the remote host
	// checking to see if the signature matches the hash provided by the user.
	if securityConfig.RootCACert == nil {
		rootCACert, err := GetRemoteCA(managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// Save the rootCA to disk.
		pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
		if err := ioutil.WriteFile(pathToRootCACert, rootCACert, 0644); err != nil {
			return nil, err
		}
		securityConfig.RootCACert = rootCACert
	}

	// Create a new key/pair and CSR
	pathToCSR := filepath.Join(baseCertDir, agentCSRFilename)
	pathToTLSCert := filepath.Join(baseCertDir, agentTLSCertFilename)
	pathToTLSKey := filepath.Join(baseCertDir, agentTLSKeyFilename)
	csr, _, err := GenerateAndWriteNewCSR(pathToCSR, pathToTLSKey)
	if err != nil {
		return nil, err
	}

	// Get a certificate issued by this CA
	signedCert, err := getSignedCertificate(csr, securityConfig.RootCACert, "agent", managerAddr)
	if err != nil {
		return nil, err
	}

	// Append the root certificate to the certificate to create a valid chain
	certChain := append(signedCert, securityConfig.RootCACert...)

	// Write the chain to disk
	if err := ioutil.WriteFile(pathToTLSCert, certChain, 0644); err != nil {
		return nil, err
	}

	// Load the agent key pair into a tls.Certificate
	tlsKeyPair, err := tls.LoadX509KeyPair(pathToTLSCert, pathToTLSKey)
	if err != nil {
		return nil, err
	}

	// Create a client TLSConfig out of the key pair
	tlsConfig, err := NewClientTLSConfig(&tlsKeyPair, securityConfig.RootCACert, "manager")
	if err != nil {
		return nil, err
	}

	securityConfig.ClientCert = &tlsKeyPair
	securityConfig.ClientTLSCreds = credentials.NewTLS(tlsConfig)

	return securityConfig, nil
}

// LoadOrCreateManagerSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as a Manager. Every manager requires at least a set of TLS certificates with which to serve
// the cluster and the dispatcher's server. Additionally, passing the CA param as true means that
// this manager will also be taking intermediate CA responsabilities, and should ensure to request
// an intermediate certificate. Finally, if no manager addresses are provided, we assume we're
// creating a new Cluster, and this manager will have to generate its own self-signed CA.
func LoadOrCreateManagerSecurityConfig(baseCertDir, caHash, managerAddr string, CA bool) (*ManagerSecurityConfig, error) {
	pathToTLSCert := filepath.Join(baseCertDir, managerTLSCertFilename)
	pathToTLSKey := filepath.Join(baseCertDir, managerTLSKeyFilename)
	pathToCSR := filepath.Join(baseCertDir, managerCSRFilename)
	pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
	pathToRootCAKey := filepath.Join(baseCertDir, rootCAKeyFilename)

	// Attempt to load the current files available on this baseDir
	securityConfig := loadManagerSecurityConfig(baseCertDir)
	err := securityConfig.validate()
	if err == nil {
		return securityConfig, nil
	}
	log.Debugf("failed to validate the manager config: %v\n", err)

	switch err.(type) {
	case NoSwarmCAError:
		// We have no CA and no remote managers are being passed in, means we're creating a new cluster
		if managerAddr == "" {
			signer, rootCACert, err := CreateRootCA(pathToRootCACert, pathToRootCAKey, rootCN)
			if err != nil {
				return nil, err
			}

			// Create a new random ID for this manager
			managerID := identity.NewID()

			// Generate TLS Certificates using the local Root
			serverCert, err := GenerateAndSignNewTLSCert(signer, rootCACert, pathToTLSCert, pathToTLSKey, managerID, "manager")
			if err != nil {
				return nil, err
			}

			// Create the TLS Credentials for this manager
			serverTLSConfig, err := NewServerTLSConfig(serverCert, rootCACert)
			if err != nil {
				return nil, err
			}

			// Create a TLSConfig to be used when this manager connects as a client to another remote manager
			clientTLSConfig, err := NewClientTLSConfig(serverCert, rootCACert, "manager")
			if err != nil {
				return nil, err
			}

			// Change our Security Config to reflect our current state
			securityConfig.Signer = signer
			securityConfig.RootCA = true
			securityConfig.RootCACert = rootCACert
			securityConfig.ServerCert = serverCert
			securityConfig.ServerTLSCreds = credentials.NewTLS(serverTLSConfig)
			securityConfig.ClientTLSCreds = credentials.NewTLS(clientTLSConfig)

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
	if securityConfig.RootCACert == nil {
		rootCACert, err := GetRemoteCA(managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// Save the rootCA to disk.
		pathToRootCACert := filepath.Join(baseCertDir, rootCACertFilename)
		if err := ioutil.WriteFile(pathToRootCACert, rootCACert, 0644); err != nil {
			return nil, err
		}
		securityConfig.RootCACert = rootCACert
	}

	// Create a new key/pair and CSR for the new manager
	csr, key, err := GenerateAndWriteNewCSR(pathToCSR, pathToTLSKey)
	if err != nil {
		log.Debugf("error when generating new node certs: %v\n", err)
		return nil, err
	}

	// Get the remote manager to issue a CA signed certificate for this node
	signedCert, err := getSignedCertificate(csr, securityConfig.RootCACert, "manager", managerAddr)
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
	serverTLSConfig, err := NewServerTLSConfig(&tlsKeyPair, securityConfig.RootCACert)
	if err != nil {
		return nil, err
	}

	// Create a TLSConfig to be used when this manager connects as a client to another remote manager
	clientTLSConfig, err := NewClientTLSConfig(&tlsKeyPair, securityConfig.RootCACert, "manager")
	if err != nil {
		return nil, err
	}

	securityConfig.ServerCert = &tlsKeyPair
	securityConfig.ServerTLSCreds = credentials.NewTLS(serverTLSConfig)
	securityConfig.ClientTLSCreds = credentials.NewTLS(clientTLSConfig)

	return securityConfig, nil
}

// NewServerTLSConfig returns a tls.Config configured for a TLS Server, given a tls.Certificate
// and the PEM-encoded root CA Certificate
func NewServerTLSConfig(cert *tls.Certificate, rootCAPEM []byte) (*tls.Config, error) {
	if rootCAPEM == nil {
		return nil, fmt.Errorf("no root PEM certificate provided")
	}
	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(rootCAPEM); !ok {
		return nil, fmt.Errorf("failed to parse PEM data to pool")
	}

	return &tls.Config{
		Certificates:             []tls.Certificate{*cert},
		ClientAuth:               tls.VerifyClientCertIfGiven,
		RootCAs:                  certPool,
		ClientCAs:                certPool,
		PreferServerCipherSuites: true,
		MinVersion:               tls.VersionTLS12,
	}, nil
}

// NewClientTLSConfig returns a tls.Config configured for a TLS Client, given a tls.Certificate
// the PEM-encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func NewClientTLSConfig(cert *tls.Certificate, rootCAPEM []byte, serverName string) (*tls.Config, error) {
	if rootCAPEM == nil {
		return nil, fmt.Errorf("no root PEM certificate provided")
	}
	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(rootCAPEM); !ok {
		return nil, fmt.Errorf("failed to parse PEM data to pool")
	}

	return &tls.Config{
		ServerName:   serverName,
		Certificates: []tls.Certificate{*cert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
	}, nil
}

// NewClientTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func NewClientTLSCredentials(cert *tls.Certificate, rootCAPEM []byte, serverName string) (credentials.TransportAuthenticator, error) {
	if rootCAPEM == nil {
		return nil, fmt.Errorf("no root PEM certificate provided")
	}
	certPool := x509.NewCertPool()

	if ok := certPool.AppendCertsFromPEM(rootCAPEM); !ok {
		return nil, fmt.Errorf("failed to parse PEM data to pool")
	}

	tlsConfig := &tls.Config{
		ServerName:   serverName,
		Certificates: []tls.Certificate{*cert},
		RootCAs:      certPool,
		MinVersion:   tls.VersionTLS12,
	}

	return credentials.NewTLS(tlsConfig), nil
}

func getSignedCertificate(csr, rootCACert []byte, nodeType, managerAddr string) ([]byte, error) {
	opts := []grpc.DialOption{}

	if rootCACert == nil {
		// Create grpc TLS options that are insecure for our first Certificate
		// request
		insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
		opts = append(opts, grpc.WithTransportCredentials(insecureCreds))

	} else {
		cp := x509.NewCertPool()
		if !cp.AppendCertsFromPEM(rootCACert) {
			return nil, fmt.Errorf("credentials: failed to append certificates")
		}

		creds := credentials.NewTLS(&tls.Config{ServerName: "manager", RootCAs: cp})
		opts = append(opts, grpc.WithTransportCredentials(creds))
	}

	// TODO(diogo): Add a connection picker
	conn, err := grpc.Dial(managerAddr, opts...)
	if err != nil {
		return nil, err
	}
	// Create a CAClient to retreive a new Certificate
	caCLient := api.NewCAClient(conn)

	// Create a context for our GRPC call
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Send the Request and retrieve the certificate
	request := &api.IssueCertificateRequest{CSR: csr, NodeType: nodeType}
	response, err := caCLient.IssueCertificate(ctx, request)
	if err != nil {
		return nil, err
	}

	return response.CertificateChain, nil
}
