package ca

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"sync"
	"time"

	log "github.com/Sirupsen/logrus"
	cfconfig "github.com/cloudflare/cfssl/config"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/picker"

	"golang.org/x/net/context"
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

var (
	//TODO(diogo): replace this with a sane renewal time
	defaultRenewalTime = 30 * time.Second
)

// SecurityConfig is used to represent a node's security configuration. It includes information about
// the RootCA and ServerTLSCreds/ClientTLSCreds transport authenticators to be used for MTLS
type SecurityConfig struct {
	mu sync.Mutex

	rootCA *RootCA

	ServerTLSCreds *MutableTLSCreds
	ClientTLSCreds *MutableTLSCreds
}

// CertificateUpdate represents a change in the underlying TLS configuration being returned by
// a certificate renewal event.
type CertificateUpdate struct {
	Role string
	Err  error
}

// NewSecurityConfig initializes and returns a new SecurityConfig.
func NewSecurityConfig(rootCA *RootCA, clientTLSCreds, serverTLSCreds *MutableTLSCreds) *SecurityConfig {
	return &SecurityConfig{
		rootCA:         rootCA,
		ClientTLSCreds: clientTLSCreds,
		ServerTLSCreds: serverTLSCreds,
	}
}

// RootCA returns the root CA.
func (s *SecurityConfig) RootCA() *RootCA {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.rootCA
}

// UpdateRootCA replaces the root CA with a new root CA based on the specified
// certificate and key.
func (s *SecurityConfig) UpdateRootCA(cert, key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	rootCA, err := NewRootCA(cert, key)
	if err == nil {
		s.rootCA = &rootCA
	}

	return err
}

// DefaultPolicy is the default policy used by the signers to ensure that the only fields
// from the remote CSRs we trust are: PublicKey, PublicKeyAlgorithm and SignatureAlgorithm.
var DefaultPolicy = func() *cfconfig.Signing {
	return &cfconfig.Signing{
		Default: &cfconfig.SigningProfile{
			Usage: []string{"signing", "key encipherment", "server auth", "client auth"},
			// TODO(diogo): change to 3 months of expiry
			// ExpiryString: "2160h",
			// Expiry:       2160 * time.Hour,
			ExpiryString: "1h",
			Expiry:       1 * time.Hour,
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
			Key:  filepath.Join(baseCertDir, nodeTLSKeyFilename)},
		RootCA: CertPaths{
			Cert: filepath.Join(baseCertDir, rootCACertFilename),
			Key:  filepath.Join(baseCertDir, rootCAKeyFilename)},
	}
}

// LoadOrCreateSecurityConfig encapsulates the security logic behind joining a cluster.
// Every node requires at least a set of TLS certificates with which to join the cluster with.
// In the case of a manager, these certificates will be used both for client and server credentials.
func LoadOrCreateSecurityConfig(ctx context.Context, baseCertDir, caHash, proposedRole string, picker *picker.Picker) (*SecurityConfig, error) {
	paths := NewConfigPaths(baseCertDir)

	var (
		rootCA                         RootCA
		serverTLSCreds, clientTLSCreds *MutableTLSCreds
		err                            error
	)

	// Check if we already have a CA certificate on disk. We need a CA to have a valid SecurityConfig
	rootCA, err = GetLocalRootCA(baseCertDir)
	switch err {
	case nil:
		log.Debugf("loaded local CA certificate: %s.", paths.RootCA.Cert)
	case ErrNoLocalRootCA:
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
	default:
		return nil, err
	}

	// At this point we've successfully loaded the CA details from disk, or successfully
	// downloaded them remotely.
	// The next step is to try to load our certificates.
	clientTLSCreds, serverTLSCreds, err = LoadTLSCreds(rootCA, paths.Node)
	if err != nil {
		log.Debugf("no valid local TLS credentials found: %v", err)

		var (
			tlsKeyPair *tls.Certificate
			err        error
		)

		if rootCA.CanSign() {
			// Create a new random ID for this certificate
			cn := identity.NewNodeID()

			tlsKeyPair, err = rootCA.IssueAndSaveNewCertificates(paths.Node, cn, proposedRole)
		} else {
			// There was an error loading our Credentials, let's get a new certificate issued
			// Last argument is nil because at this point we don't have any valid TLS creds
			tlsKeyPair, err = rootCA.RequestAndSaveNewCertificates(ctx, paths.Node, proposedRole, picker, nil)
			if err != nil {
				return nil, err
			}

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

	return &SecurityConfig{
		rootCA: &rootCA,

		ServerTLSCreds: serverTLSCreds,
		ClientTLSCreds: clientTLSCreds,
	}, nil
}

// RenewTLSConfig will continuously monitor for the necessity of renewing the local certificates, either by
// issuing them locally if key-material is available, or requesting them from a remote CA.
func RenewTLSConfig(ctx context.Context, s *SecurityConfig, baseCertDir string, picker *picker.Picker, retry time.Duration, renew <-chan struct{}) <-chan CertificateUpdate {
	paths := NewConfigPaths(baseCertDir)
	updates := make(chan CertificateUpdate)
	go func() {
		defer close(updates)
		for {
			select {
			case <-time.After(retry):
			case <-renew:
			case <-ctx.Done():
				return
			}

			// Retrieve the number of months left for the cert to expire
			expMonths, err := readCertExpiration(paths.Node)
			if err != nil {
				log.Debugf("failed to read expiration of TLS Certificate: %v", err)
				updates <- CertificateUpdate{Err: err}
				continue
			}

			// Check if the certificate is close to expiration.
			if expMonths > 1 {
				continue
			}

			log.Debugf("Renewing TLS Certificates.")

			rootCA := s.RootCA()

			// We are dependent on an external node, let's request new certs
			tlsKeyPair, err := rootCA.RequestAndSaveNewCertificates(ctx,
				paths.Node,
				s.ClientTLSCreds.Role(),
				picker,
				s.ClientTLSCreds)
			if err != nil {
				log.Debugf("failed to get a tlsKeyPair: %v", err)
				updates <- CertificateUpdate{Err: err}
				continue
			}

			clientTLSConfig, err := NewClientTLSConfig(tlsKeyPair, rootCA.Pool, CARole)
			if err != nil {
				log.Debugf("failed to create a new client TLS config: %v", err)
				updates <- CertificateUpdate{Err: err}
			}
			serverTLSConfig, err := NewServerTLSConfig(tlsKeyPair, rootCA.Pool)
			if err != nil {
				log.Debugf("failed to create a new server TLS config: %v", err)
				updates <- CertificateUpdate{Err: err}
			}

			err = s.ClientTLSCreds.LoadNewTLSConfig(clientTLSConfig)
			if err != nil {
				log.Debugf("failed to update the client TLS credentials: %v", err)
				updates <- CertificateUpdate{Err: err}
			}

			err = s.ServerTLSCreds.LoadNewTLSConfig(serverTLSConfig)
			if err != nil {
				log.Debugf("failed to update the server TLS credentials: %v", err)
				updates <- CertificateUpdate{Err: err}
			}

			updates <- CertificateUpdate{Role: s.ClientTLSCreds.Role()}
		}
	}()

	return updates
}

// LoadTLSCreds loads tls credentials from the specified path and verifies that
// thay are valid for the RootCA.
func LoadTLSCreds(rootCA RootCA, paths CertPaths) (*MutableTLSCreds, *MutableTLSCreds, error) {
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
	var (
		keyPair tls.Certificate
		newErr  error
	)
	keyPair, err = tls.X509KeyPair(cert, key)
	if err != nil {
		// This current keypair isn't valid. It's possible we crashed before we
		// overwrote the current key. Let's try loading it from disk.
		tempPaths := genTempPaths(paths)
		key, newErr = ioutil.ReadFile(tempPaths.Key)
		if newErr != nil {
			return nil, nil, err
		}

		keyPair, newErr = tls.X509KeyPair(cert, key)
		if err != nil {
			return nil, nil, err
		}
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

func genTempPaths(path CertPaths) CertPaths {
	return CertPaths{
		Key:  filepath.Join(filepath.Dir(path.Key), "."+filepath.Base(path.Key)),
		Cert: filepath.Join(filepath.Dir(path.Cert), "."+filepath.Base(path.Cert)),
	}
}

// NewServerTLSConfig returns a tls.Config configured for a TLS Server, given a tls.Certificate
// and the PEM-encoded root CA Certificate
func NewServerTLSConfig(cert *tls.Certificate, rootCAPool *x509.CertPool) (*tls.Config, error) {
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

// NewClientTLSConfig returns a tls.Config configured for a TLS Client, given a tls.Certificate
// the PEM-encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func NewClientTLSConfig(cert *tls.Certificate, rootCAPool *x509.CertPool, serverName string) (*tls.Config, error) {
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
func (rca *RootCA) NewClientTLSCredentials(cert *tls.Certificate, serverName string) (*MutableTLSCreds, error) {
	tlsConfig, err := NewClientTLSConfig(cert, rca.Pool, serverName)
	if err != nil {
		return nil, err
	}

	mtls, err := NewMutableTLS(tlsConfig)

	return mtls, err
}

// NewServerTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func (rca *RootCA) NewServerTLSCredentials(cert *tls.Certificate) (*MutableTLSCreds, error) {
	tlsConfig, err := NewServerTLSConfig(cert, rca.Pool)
	if err != nil {
		return nil, err
	}

	mtls, err := NewMutableTLS(tlsConfig)

	return mtls, err
}
