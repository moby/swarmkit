package ca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/docker/swarm-v2/picker"

	"golang.org/x/net/context"

	"google.golang.org/grpc/credentials"
)

const (
	rootCACertFilename  = "swarm-root-ca.crt"
	rootCAKeyFilename   = "swarm-root-ca.key"
	nodeTLSCertFilename = "swarm-node.crt"
	nodeTLSKeyFilename  = "swarm-node.key"
	nodeCSRFilename     = "swarm-node.csr"
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

// SecurityConfig is used to represent a node's security configuration. It includes information about
// the RootCA and ServerTLSCreds/ClientTLSCreds transport authenticators to be used for MTLS
type SecurityConfig struct {
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
	Node, RootCA CertPaths
}

// NewConfigPaths returns the absolute paths to all of the different types of files
func NewConfigPaths(baseCertDir string) *SecurityConfigPaths {
	return &SecurityConfigPaths{
		Node: CertPaths{
			Cert: filepath.Join(baseCertDir, nodeTLSCertFilename),
			Key:  filepath.Join(baseCertDir, nodeTLSKeyFilename),
			CSR:  filepath.Join(baseCertDir, nodeCSRFilename)},
		RootCA: CertPaths{
			Cert: filepath.Join(baseCertDir, rootCACertFilename),
			Key:  filepath.Join(baseCertDir, rootCAKeyFilename)},
	}
}

// LoadOrCreateSecurityConfig encapsulates the security logic behind starting or joining a cluster.
// Every node requires at least a set of TLS certificates with which to join the cluster with.
// In the case of a manager, these certificates will be used both for client and server credentials.
func LoadOrCreateSecurityConfig(ctx context.Context, baseCertDir, caHash, proposedRole string, picker *picker.Picker) (*SecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		rootCA                         RootCA
		serverTLSCreds, clientTLSCreds credentials.TransportAuthenticator
		err                            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have a valid SecurityConfig
	rootCA, err = GetLocalRootCA(paths.RootCA)
	if err != nil {
		log.Debugf("no valid local CA certificate found: %v", err)

		// Get the remote CA certificate, verify integrity with the hash provided
		rootCA, err = GetRemoteCA(ctx, caHash, picker)
		if err != nil {
			return nil, err
		}

		// Save root CA certificate to disk
		if err = saveRootCA(rootCA, paths.RootCA); err != nil {
			return nil, err
		}

		log.Debugf("downloaded remote CA certificate.")

	} else {
		log.Debugf("loaded local CA certificate: %s.", paths.RootCA.Cert)
	}

	// At this point we've successfully loaded the CA details from disk, or successfully
	// downloaded them remotely.
	// The next step is to try to load our certificates.
	clientTLSCreds, serverTLSCreds, err = loadTLSCreds(rootCA, paths.Node)
	if err != nil {
		log.Debugf("no valid local TLS credentials found: %v", err)

		// There was an error loading our Credentials, let's get a new certificate issued
		tlsKeyPair, err := rootCA.IssueAndSaveNewCertificates(ctx, paths.Node, proposedRole, picker)
		if err != nil {
			return nil, err
		}

		// Create the Server TLS Credentials for this node. These will not be used by agents.
		serverTLSCreds, err = rootCA.NewServerTLSCredentials(tlsKeyPair)
		if err != nil {
			return nil, err
		}

		// Create a TLSConfig to be used when this node connects as a client to another remote node.
		// We're using ManagerRole as remote serverName for TLS host verification
		clientTLSCreds, err = rootCA.NewClientTLSCredentials(tlsKeyPair, ManagerRole)
		if err != nil {
			return nil, err
		}
		log.Debugf("new TLS credentials generated: %s.", paths.Node.Cert)
	} else {
		log.Debugf("loaded local TLS credentials: %s.", paths.Node.Cert)
	}

	return &SecurityConfig{RootCA: rootCA, ServerTLSCreds: serverTLSCreds, ClientTLSCreds: clientTLSCreds}, nil
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

	// Load the Certificates as server credentials
	serverTLSCreds, err := rootCA.NewServerTLSCredentials(&keyPair)
	if err != nil {
		return nil, nil, err
	}

	// Load the Certificates also as client credentials.
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
