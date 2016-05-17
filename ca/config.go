package ca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	cfconfig "github.com/cloudflare/cfssl/config"

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
	rootCN = "swarm-ca"
	// ManagerRole represents the Manager node type, and is used for authorization to endpoints
	ManagerRole = "swarm-manager"
	// AgentRole represents the Agent node type, and is used for authorization to endpoints
	AgentRole = "swarm-worker"
	// CARole represents the CA node type, and is used for clients attempting to get new certificates issued
	CARole = "swarm-ca"
)

// AgentSecurityConfig is used to configure the security params of the agents
type AgentSecurityConfig struct {
	RootCA

	ClientTLSCreds credentials.TransportAuthenticator
}

// ManagerSecurityConfig is used to configure the CA stack of the manager
type ManagerSecurityConfig struct {
	RootCA

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
		Agent: CertPaths{
			Cert: filepath.Join(baseCertDir, agentTLSCertFilename),
			Key:  filepath.Join(baseCertDir, agentTLSKeyFilename),
			CSR:  filepath.Join(baseCertDir, agentCSRFilename)},
		Manager: CertPaths{
			Cert: filepath.Join(baseCertDir, managerTLSCertFilename),
			Key:  filepath.Join(baseCertDir, managerTLSKeyFilename),
			CSR:  filepath.Join(baseCertDir, managerCSRFilename)},
		RootCA: CertPaths{
			Cert: filepath.Join(baseCertDir, rootCACertFilename),
			Key:  filepath.Join(baseCertDir, rootCAKeyFilename)},
	}
}

// LoadOrCreateAgentSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as an Agent. Every agent requires at least a set of TLS certificates with which to join
// the cluster.
func LoadOrCreateAgentSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string) (*AgentSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		rootCA         RootCA
		clientTLSCreds credentials.TransportAuthenticator
		err            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have
	// a valid SecurityConfig
	rootCA, err = GetLocalRootCA(paths.RootCA)
	if err != nil {
		log.Debugf("no valid local CA certificate found: %v", err)
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
		rootCA, err = GetRemoteCA(ctx, managerAddr, caHash)
		if err != nil {
			return nil, err
		}

		// If the root certificate got returned successfully, save the rootCA to disk.
		if err = ioutil.WriteFile(paths.RootCA.Cert, rootCA.Cert, 0644); err != nil {
			return nil, err
		}

		log.Debugf("downloaded remote CA certificate from: %s", managerAddr)
	} else {
		log.Debugf("loaded local CA certificate from: %v", paths.RootCA.Cert)
	}

	// At this point we either had, or successfully retrieved a CA.
	// The next step is to try to load our certificates.
	clientTLSCreds, _, err = loadTLSCreds(rootCA, paths.Agent)
	if err != nil {
		log.Debugf("no valid local TLS credentials found: %v", err)
		// There was an error loading our Credentials, let's get a new certificate reissued
		// Contact the remote CA, get a new certificate issued and save it to disk
		tlsKeyPair, err := rootCA.IssueAndSaveNewCertificates(ctx, paths.Agent, AgentRole, managerAddr)
		if err != nil {
			return nil, err
		}

		// Create a TLSConfig to be used when this manager connects as a client to another remote manager
		clientTLSCreds, err = rootCA.NewClientTLSCredentials(tlsKeyPair, ManagerRole)
		if err != nil {
			return nil, err
		}

		log.Debugf("new TLS credentials generated: %s.", paths.Agent.Cert)
	} else {
		log.Debugf("loaded local TLS credentials: %v", paths.Agent.Cert)
	}

	return &AgentSecurityConfig{ClientTLSCreds: clientTLSCreds, RootCA: rootCA}, nil
}

// LoadOrCreateManagerSecurityConfig encapsulates the security logic behind starting or joining a cluster
// as a Manager. Every manager requires at least a set of TLS certificates with which to serve
// the cluster and the dispatcher's server. If no manager addresses are provided, we assume we're
// creating a new Cluster, and this manager will have to generate its own self-signed CA.
func LoadOrCreateManagerSecurityConfig(ctx context.Context, baseCertDir, caHash, managerAddr string) (*ManagerSecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		rootCA                         RootCA
		serverTLSCreds, clientTLSCreds credentials.TransportAuthenticator
		err                            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have
	// a valid SecurityConfig
	rootCA, err = GetLocalRootCA(paths.RootCA)
	if err != nil {
		log.Debugf("no valid local CA certificate found: %v", err)

		// Make sure the necessary dirs exist and they are writable
		err = os.MkdirAll(baseCertDir, 0755)
		if err != nil {
			return nil, err
		}

		// We have no CA and no remote managers are being passed in, means we're creating a new cluster
		// Create our new RootCA and write everything to disk
		if managerAddr == "" {
			rootCA, err = CreateAndWriteRootCA(rootCN, paths.RootCA)
			if err != nil {
				return nil, err
			}
			log.Debugf("generating a new CA with CN=%s, using a %d bit %s key.", rootCN, RootKeySize, RootKeyAlgo)
		} else {
			// If we've been passed the address of a remote manager to join, attempt to retrieve the remote
			// root CA details
			rootCA, err = GetRemoteCA(ctx, managerAddr, caHash)
			if err != nil {
				return nil, err
			}

			// If the root certificate got returned successfully, save the rootCA to disk.
			if err = ioutil.WriteFile(paths.RootCA.Cert, rootCA.Cert, 0644); err != nil {
				return nil, err
			}
			log.Infof("Waiting for cluster membership to be approved to proceed with further action")
			log.Debugf("downloaded remote CA certificate from: %s", managerAddr)
		}

	} else {
		log.Debugf("loaded local CA certificate: %s.", paths.RootCA.Cert)
	}

	// At this point we either fully boostraped the first Manager, or successfully retrieved a CA.
	// The next step is to try to load our certificates.
	clientTLSCreds, serverTLSCreds, err = loadTLSCreds(rootCA, paths.Manager)
	if err != nil {
		log.Debugf("no valid local TLS credentials found: %v", err)

		// There was an error loading our Credentials, let's get a new certificate reissued
		// Contact the remote CA, get a new certificate issued and save it to disk
		tlsKeyPair, err := rootCA.IssueAndSaveNewCertificates(ctx, paths.Manager, ManagerRole, managerAddr)
		if err != nil {
			return nil, err
		}

		// Create the TLS Credentials for this manager
		serverTLSCreds, err = rootCA.NewServerTLSCredentials(tlsKeyPair)
		if err != nil {
			return nil, err
		}

		// Create a TLSConfig to be used when this manager connects as a client to another remote manager
		clientTLSCreds, err = rootCA.NewClientTLSCredentials(tlsKeyPair, ManagerRole)
		if err != nil {
			return nil, err
		}
		log.Debugf("new TLS credentials generated: %s.", paths.Manager.Cert)
	} else {
		log.Debugf("loaded local TLS credentials: %s.", paths.Manager.Cert)
	}

	return &ManagerSecurityConfig{RootCA: rootCA, ServerTLSCreds: serverTLSCreds, ClientTLSCreds: clientTLSCreds}, nil
}

func loadTLSCreds(rootCA RootCA, paths CertPaths) (credentials.TransportAuthenticator, credentials.TransportAuthenticator, error) {
	// Read both the Cert and Key from disk
	cert, err := ioutil.ReadFile(paths.Cert)
	if err != nil {
		return nil, nil, err
	}
	key, err := ioutil.ReadFile(paths.Key)
	if err != nil {
		return nil, nil, err
	}

	// Create an x509 certificate out of the contents on disk
	certBlock, _ := pem.Decode([]byte(cert))
	if certBlock == nil {
		return nil, nil, fmt.Errorf("failed to parse certificate PEM")
	}

	// Create an X509Cert so we can .Verify()
	X509Cert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}

	// Include our root pool
	opts := x509.VerifyOptions{
		Roots: rootCA.Pool,
	}

	// Check to see if this certificate was signed by our CA, and isn't expired
	if _, err := X509Cert.Verify(opts); err != nil {
		return nil, nil, err
	}

	// Now that we know this certificate is valid, create a TLS Certificate for our
	// credentials
	keyPair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, nil, err
	}

	// Load the manager Certificates as server credentials
	serverTLSCreds, err := rootCA.NewServerTLSCredentials(&keyPair)
	if err != nil {
		return nil, nil, err
	}

	// Load the manager Certificates also as client credentials.
	// Both Agents and Managers always connect to remote Managers,
	// so ServerName is always set to ManagerRole here.
	clientTLSCreds, err := rootCA.NewClientTLSCredentials(&keyPair, ManagerRole)
	if err != nil {
		return nil, nil, err
	}

	return clientTLSCreds, serverTLSCreds, nil
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
