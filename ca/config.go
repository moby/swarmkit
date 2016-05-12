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
	"github.com/docker/swarm-v2/identity"

	"golang.org/x/net/context"

	"google.golang.org/grpc/credentials"
)

const (
	rootCACertFilename     = "swarm-root-ca.crt"
	rootCAKeyFilename      = "swarm-root-ca.key"
	managerTLSCertFilename = "swarm-manager.crt"
	managerTLSKeyFilename  = "swarm-manager.key"
	managerCSRFilename     = "swarm-manager.csr"
	agentTLSCertFilename   = "swarm-worker.crt"
	agentTLSKeyFilename    = "swarm-worker.key"
	agentCSRFilename       = "swarm-worker.csr"
)

const (
	rootCN = "cluster-ca"
	// ManagerRole represents the Manager node type, and is used for authorization to endpoints
	ManagerRole = "cluster-manager"
	// AgentRole represents the Agent node type, and is used for authorization to endpoints
	AgentRole = "cluster-worker"
	// CARole represents the CA node type, and is used for clients attempting to get new certificates issued
	CARole = "cluster-ca"
)

// AgentSecurityConfig is used to configure the security params of the agents
type AgentSecurityConfig struct {
	RootCAPool     *x509.CertPool
	ClientTLSCreds credentials.TransportAuthenticator
}

// Signer is the representation of everything we need to sign certificates
type Signer struct {
	RootCACert   []byte
	RootCAPool   *x509.CertPool
	CryptoSigner signer.Signer
}

// CanSign ensures that the signer has all three necessary elements needed to operate
func (ca *Signer) CanSign() bool {
	if ca.RootCAPool == nil || ca.RootCACert == nil {
		return false
	}

	return !(ca.CryptoSigner == nil)
}

// ManagerSecurityConfig is used to configure the CA stack of the manager
type ManagerSecurityConfig struct {
	Signer

	ServerTLSCreds credentials.TransportAuthenticator
	ClientTLSCreds credentials.TransportAuthenticator
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

// SecurityConfigPaths is used as a helper to hold all the paths of security relevant files
type SecurityConfigPaths struct {
	Agent, Manager, RootCA CertPaths
}

// NewConfigPaths returns the absolute paths to all of the different types of files
func NewConfigPaths(baseCertDir string) *SecurityConfigPaths {
	return &SecurityConfigPaths{
		Agent: CertPaths{Cert: filepath.Join(baseCertDir, agentTLSCertFilename),
			Key: filepath.Join(baseCertDir, agentTLSKeyFilename),
			CSR: filepath.Join(baseCertDir, agentCSRFilename)},
		Manager: CertPaths{Cert: filepath.Join(baseCertDir, managerTLSCertFilename),
			Key: filepath.Join(baseCertDir, managerTLSKeyFilename),
			CSR: filepath.Join(baseCertDir, managerCSRFilename)},
		RootCA: CertPaths{Cert: filepath.Join(baseCertDir, rootCACertFilename),
			Key: filepath.Join(baseCertDir, rootCAKeyFilename)},
	}
}

// LoadOrCreateAgentSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as an Agent. Every agent requires at least a set of TLS certificates with which to join
// the cluster.
func LoadOrCreateAgentSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		signer         Signer
		clientTLSCreds credentials.TransportAuthenticator
		err            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have
	// a valid SecurityConfig
	signer, err = GetRootCA(paths.RootCA)
	if err != nil {
		log.Debugf("error loading CA certificate: %v", err)
		// Make sure the necessary dirs exist and they are writable
		err = os.MkdirAll(baseCertDir, 0755)
		if err != nil {
			return nil, err
		}

		// We don't have a CA configured. Let's get it from the remote manager
		// We need a remote manager to be able to download the remote CA
		if managerAddr == "" {
			return nil, fmt.Errorf("address of a manager is required to join a cluster")
		}

		// We were provided with a remote manager. Lets try retreiving the remote CA
		signer, err = GetRemoteCA(ctx, managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// If the root certificate got returned successfully, save the rootCA to disk.
		if err = ioutil.WriteFile(paths.RootCA.Cert, signer.RootCACert, 0644); err != nil {
			return nil, err
		}

		log.Debugf("downloaded remote CA certificate from: %v", managerAddr)
	} else {
		log.Debugf("loaded local CA certificate from: %v", paths.RootCA.Cert)
	}

	// At this point we either had, or successfully retrieved a CA.
	// The next step is to try to load our certificates.
	_, clientTLSCreds, err = loadTLSCreds(signer, paths.Agent)
	if err != nil {
		log.Debugf("error loading TLS credentials: %v", err)
		// There was an error loading our Credentials, let's get a new certificate reissued
		// Contact the remote CA, get a new certificate issued and save it to disk
		tlsKeyPair, err := issueAndSaveNewCertificates(ctx, paths.Agent, AgentRole, managerAddr, signer.RootCAPool)
		if err != nil {
			return nil, err
		}

		// Create a TLSConfig to be used when this manager connects as a client to another remote manager
		clientTLSCreds, err = NewClientTLSCredentials(tlsKeyPair, signer.RootCAPool, ManagerRole)
		if err != nil {
			return nil, err
		}

		log.Debugf("retrieved TLS credentials from: %v", managerAddr)
	} else {
		log.Debugf("loaded local TLS credentials: %v", paths.Agent.Cert)
	}

	return &AgentSecurityConfig{ClientTLSCreds: clientTLSCreds, RootCAPool: signer.RootCAPool}, nil
}

// LoadOrCreateManagerSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as a Manager. Every manager requires at least a set of TLS certificates with which to serve
// the cluster and the dispatcher's server. If no manager addresses are provided, we assume we're
// creating a new Cluster, and this manager will have to generate its own self-signed CA.
func LoadOrCreateManagerSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		signer                         Signer
		serverTLSCreds, clientTLSCreds credentials.TransportAuthenticator
		err                            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have
	// a valid SecurityConfig
	signer, err = GetRootCA(paths.RootCA)
	if err != nil {
		log.Debugf("error loading CA certificate: %v", err)

		// Make sure the necessary dirs exist and they are writable
		err = os.MkdirAll(baseCertDir, 0755)
		if err != nil {
			return nil, err
		}

		// We have no CA and no remote managers are being passed in, means we're creating a new cluster
		// Create our new RootCA, manager certificates, and write everything to disk
		if managerAddr == "" {
			log.Debugf("bootstraping a new cluster...")
			return fullCAManagerBootstrap(rootCN, paths)
		}

		// If we've been passed the address of a remote manager to join, attempt to retrieve the remote
		// root CA details
		signer, err = GetRemoteCA(ctx, managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// If the root certificate got returned successfully, save the rootCA to disk.
		if err = ioutil.WriteFile(paths.RootCA.Cert, signer.RootCACert, 0644); err != nil {
			return nil, err
		}
		log.Debugf("downloaded remote CA certificate from: %v", managerAddr)
	} else {
		log.Debugf("loaded local CA certificate from: %v", paths.RootCA.Cert)
	}

	// At this point we either fully boostraped the first Manager, or successfully retrieved a CA.
	// The next step is to try to load our certificates.
	serverTLSCreds, clientTLSCreds, err = loadTLSCreds(signer, paths.Manager)
	if err != nil {
		log.Debugf("error loading TLS credentials: %v", err)

		// There was an error loading our Credentials, let's get a new certificate reissued
		// Contact the remote CA, get a new certificate issued and save it to disk
		tlsKeyPair, err := issueAndSaveNewCertificates(ctx, paths.Manager, ManagerRole, managerAddr, signer.RootCAPool)
		if err != nil {
			return nil, err
		}

		// Create the TLS Credentials for this manager
		serverTLSCreds, err = NewServerTLSCredentials(tlsKeyPair, signer.RootCAPool)
		if err != nil {
			return nil, err
		}

		// Create a TLSConfig to be used when this manager connects as a client to another remote manager
		clientTLSCreds, err = NewClientTLSCredentials(tlsKeyPair, signer.RootCAPool, ManagerRole)
		if err != nil {
			return nil, err
		}
		log.Debugf("retrieved TLS credentials from: %v", managerAddr)
	} else {
		log.Debugf("loaded local TLS credentials: %v", paths.Manager.Cert)
	}

	return &ManagerSecurityConfig{Signer: signer, ServerTLSCreds: serverTLSCreds, ClientTLSCreds: clientTLSCreds}, nil
}

func loadTLSCreds(signer Signer, paths CertPaths) (credentials.TransportAuthenticator, credentials.TransportAuthenticator, error) {
	// TODO(diogo): check expiration of certificates
	serverCert, err := tls.LoadX509KeyPair(paths.Cert, paths.Key)
	if err != nil {
		return nil, nil, err
	}

	// Load the manager Certificates as server credentials
	serverTLSCreds, err := NewServerTLSCredentials(&serverCert, signer.RootCAPool)
	if err != nil {
		return nil, nil, err
	}

	// Load the manager Certificates also as client credentials
	clientTLSCreds, err := NewClientTLSCredentials(&serverCert, signer.RootCAPool, ManagerRole)
	if err != nil {
		return nil, nil, err
	}

	return serverTLSCreds, clientTLSCreds, nil
}

func fullCAManagerBootstrap(rootCN string, paths *SecurityConfigPaths) (*ManagerSecurityConfig, error) {
	signer, err := CreateRootCA(rootCN, paths.RootCA)
	if err != nil {
		return nil, err
	}

	// Create a new random ID for this manager
	managerID := identity.NewID()

	// Generate TLS Certificates using the local Root
	_, err = GenerateAndSignNewTLSCert(signer, managerID, ManagerRole, paths.Manager)
	if err != nil {
		return nil, err
	}

	// Load the the TLS credentials from disk
	serverTLSCreds, clientTLSCreds, err := loadTLSCreds(signer, paths.Manager)
	if err != nil {
		return nil, err
	}

	return &ManagerSecurityConfig{Signer: signer, ServerTLSCreds: serverTLSCreds, ClientTLSCreds: clientTLSCreds}, nil
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
