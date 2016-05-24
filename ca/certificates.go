package ca

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	log "github.com/Sirupsen/logrus"
	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	cflog "github.com/cloudflare/cfssl/log"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/picker"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

const (
	// RootKeySize is the default size of the root CA key
	RootKeySize = 256
	// RootKeyAlgo defines the default algorithm for the root CA Key
	RootKeyAlgo = "ecdsa"
)

func init() {
	cflog.Level = 5
}

// CertPaths is a helper struct that keeps track of the paths of a
// [CSR, Cert, Key] group
type CertPaths struct {
	CSR, Cert, Key string
}

// RootCA is the representation of everything we need to sign certificates
type RootCA struct {
	Cert []byte
	Pool *x509.CertPool

	Signer signer.Signer
}

// CanSign ensures that the signer has all three necessary elements needed to operate
func (rca *RootCA) CanSign() bool {
	if rca.Cert == nil || rca.Pool == nil || rca.Signer == nil {
		return false
	}

	return true
}

// NewClientTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func (rca *RootCA) NewClientTLSCredentials(cert *tls.Certificate, serverName string) (credentials.TransportAuthenticator, error) {
	tlsConfig, err := newClientTLSConfig(cert, rca.Pool, serverName)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(tlsConfig), nil
}

// NewServerTLSCredentials returns GRPC credentials for a TLS GRPC client, given a tls.Certificate
// a PEM-Encoded root CA Certificate, and the name of the remote server the client wants to connect to.
func (rca *RootCA) NewServerTLSCredentials(cert *tls.Certificate) (credentials.TransportAuthenticator, error) {
	tlsConfig, err := newServerTLSConfig(cert, rca.Pool)
	if err != nil {
		return nil, err
	}

	return credentials.NewTLS(tlsConfig), nil
}

// IssueAndSaveNewCertificates gets new certificates issued, either by signing them locally if a signer is
// available, or by requesting them from the remote server at remoteAddr.
func (rca *RootCA) IssueAndSaveNewCertificates(ctx context.Context, paths CertPaths, role string, picker *picker.Picker) (*tls.Certificate, error) {
	// Create a new key/pair and CSR for the new manager

	csr, key, err := GenerateAndWriteNewCSR(paths)
	if err != nil {
		log.Debugf("error when generating new node certs: %v", err)
		return nil, err
	}

	var signedCert []byte
	if rca.CanSign() {
		// Create a new random ID for this certificate
		cn := identity.NewID()

		// Obtain a signed Certificate
		signedCert, err = rca.ParseValidateAndSignCSR(csr, cn, role)
		if err != nil {
			log.Debugf("failed to sign node certificate: %v", err)
			return nil, err
		}

		log.Debugf("issued TLS credentials with role: %s.", role)
	} else {
		// Get the remote manager to issue a CA signed certificate for this node
		signedCert, err = GetRemoteSignedCertificate(ctx, csr, role, rca.Pool, picker)
		if err != nil {
			return nil, err
		}

		log.Debugf("Downloaded TLS credentials with role: %s.", role)
	}

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(paths.Cert), 0755)
	if err != nil {
		return nil, err
	}

	// Write the chain to disk
	if err := ioutil.WriteFile(paths.Cert, signedCert, 0644); err != nil {
		return nil, err
	}

	// Create a valid TLSKeyPair out of the PEM encoded private key and certificate
	tlsKeyPair, err := tls.X509KeyPair(signedCert, key)
	if err != nil {
		return nil, err
	}
	return &tlsKeyPair, nil
}

// ParseValidateAndSignCSR returns a signed certificate from a particular rootCA and a CSR.
func (rca *RootCA) ParseValidateAndSignCSR(csrBytes []byte, cn, role string) ([]byte, error) {
	if !rca.CanSign() {
		return nil, fmt.Errorf("no valid signer for Root CA found")
	}

	// All managers get added the subject-alt-name of CA, so they can be used for cert issuance
	hosts := []string{role}
	if role == ManagerRole {
		hosts = append(hosts, CARole)
	}

	cert, err := rca.Signer.Sign(signer.SignRequest{
		Request: string(csrBytes),
		// OU is used for Authentication of the node type. The CN has the random
		// node ID.
		Subject: &signer.Subject{CN: cn, Names: []cfcsr.Name{{OU: role}}},
		// Adding role as DNS alt name, so clients can connect to ManagerRole and CARole
		Hosts: hosts,
	})
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	return cert, nil
}

// GetLocalRootCA validates if the contents of the file are a valid self-signed
// CA certificate, and returns the PEM-encoded Certificate if so
func GetLocalRootCA(baseDir string) (RootCA, error) {
	paths := NewConfigPaths(baseDir)

	// Check if we have a Certificate file
	cert, err := ioutil.ReadFile(paths.RootCA.Cert)
	if err != nil {
		return RootCA{}, err
	}

	// Check to see if the Certificate file is a valid, self-signed Cert
	_, err = helpers.ParseSelfSignedCertificatePEM(cert)
	if err != nil {
		return RootCA{}, err
	}

	// Create a Pool with our RootCACertificate
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		return RootCA{}, fmt.Errorf("error while adding root CA cert to Cert Pool")
	}

	// If there is a root CA keypair, we're going to try getting a crypto signer from it
	signer, err := local.NewSignerFromFile(paths.RootCA.Cert, paths.RootCA.Key, DefaultPolicy())
	if err != nil {
		return RootCA{Cert: cert, Pool: pool}, nil
	}
	log.Debugf("successfully loaded the signer for the Root CA: %s", paths.RootCA.Cert)

	return RootCA{Signer: signer, Cert: cert, Pool: pool}, nil
}

// GetRemoteCA returns the remote endpoint's CA certificate
func GetRemoteCA(ctx context.Context, hashStr string, picker *picker.Picker) (RootCA, error) {
	// We need a valid picker to be able to Dial to a remote CA
	if picker == nil {
		return RootCA{}, fmt.Errorf("valid remote address picker required")
	}

	// This TLS Config is intentionally using InsecureSkipVerify. Either we're
	// doing TOFU, in which case we don't validate the remote CA, or we're using
	// a user supplied hash to check the integrity of the CA certificate.
	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecureCreds),
		grpc.WithBackoffMaxDelay(10 * time.Second),
		grpc.WithPicker(picker)}

	firstAddr, err := picker.PickAddr()
	if err != nil {
		return RootCA{}, err
	}

	conn, err := grpc.Dial(firstAddr, opts...)
	if err != nil {
		return RootCA{}, err
	}
	defer conn.Close()

	client := api.NewCAClient(conn)
	response, err := client.GetRootCACertificate(ctx, &api.GetRootCACertificateRequest{})
	if err != nil {
		return RootCA{}, err
	}

	if hashStr != "" {
		shaHash := sha256.New()
		shaHash.Write(response.Certificate)
		md := shaHash.Sum(nil)
		mdStr := hex.EncodeToString(md)
		if hashStr != mdStr {
			return RootCA{}, fmt.Errorf("remote CA does not match fingerprint. Expected: %s, got %s", hashStr, mdStr)
		}
	}

	// Create a Pool with our RootCACertificate
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(response.Certificate) {
		return RootCA{}, fmt.Errorf("failed to append certificate to cert pool")
	}

	return RootCA{Cert: response.Certificate, Pool: pool}, nil
}

// CreateAndWriteRootCA creates a Certificate authority for a new Swarm Cluster, potentially
// overwriting any existing CAs.
func CreateAndWriteRootCA(rootCN string, paths CertPaths) (RootCA, error) {
	// Create a simple CSR for the CA using the default CA validator and policy
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: cfcsr.NewBasicKeyRequest(),
		// Expiration for the root is 20 years
		CA: &cfcsr.CAConfig{Expiry: "630720000s"},
	}

	// Generate the CA and get the certificate and private key
	cert, _, key, err := initca.New(&req)
	if err != nil {
		return RootCA{}, err
	}

	// Convert the key given by initca to an object to create a RootCA
	parsedKey, err := helpers.ParsePrivateKeyPEM(key)
	if err != nil {
		log.Errorf("failed to parse private key: %v", err)
		return RootCA{}, err
	}

	// Convert the certificate into an object to create a RootCA
	parsedCert, err := helpers.ParseCertificatePEM(cert)
	if err != nil {
		return RootCA{}, err
	}

	// Create a Signer out of the private key
	signer, err := local.NewSigner(parsedKey, parsedCert, signer.DefaultSigAlgo(parsedKey), DefaultPolicy())
	if err != nil {
		log.Errorf("failed to create signer: %v", err)
		return RootCA{}, err
	}

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(paths.Cert), 0755)
	if err != nil {
		return RootCA{}, err
	}

	// Write the Private Key and Certificate to disk, using decent permissions
	if err := ioutil.WriteFile(paths.Cert, cert, 0644); err != nil {
		return RootCA{}, err
	}
	if err := ioutil.WriteFile(paths.Key, key, 0600); err != nil {
		return RootCA{}, err
	}

	// Create a Pool with our Root CA Certificate
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(cert) {
		return RootCA{}, fmt.Errorf("failed to append certificate to cert pool")
	}

	return RootCA{Signer: signer, Cert: cert, Pool: pool}, nil
}

// BootstrapCluster receives a directory and creates both new Root CA key material
// and a ManagerRole key/certificate pair to be used by the initial cluster manager
func BootstrapCluster(baseCertDir string) error {
	paths := NewConfigPaths(baseCertDir)

	rootCA, err := CreateAndWriteRootCA(rootCN, paths.RootCA)
	if err != nil {
		return err
	}

	nodeID := identity.NewID()
	_, err = GenerateAndSignNewTLSCert(rootCA, nodeID, ManagerRole, paths.Node)

	return err
}

// GenerateAndSignNewTLSCert creates a new keypair, signs the certificate using signer,
// and saves the certificate and key to disk. This method is used to bootstrap the first
// manager TLS certificates.
func GenerateAndSignNewTLSCert(rootCA RootCA, cn, ou string, paths CertPaths) (*tls.Certificate, error) {
	// Generate and new keypair and CSR
	csr, key, err := generateNewCSR()
	if err != nil {
		return nil, err
	}

	// Obtain a signed Certificate
	cert, err := rootCA.ParseValidateAndSignCSR(csr, cn, ou)
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	// Append the root CA Key to the certificate, to create a valid chain
	certChain := append(cert, rootCA.Cert...)

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(paths.Cert), 0755)
	if err != nil {
		return nil, err
	}

	// Write both the chain and key to disk
	if err := ioutil.WriteFile(paths.Cert, certChain, 0644); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(paths.Key, key, 0600); err != nil {
		return nil, err
	}

	// Load a valid tls.Certificate from the chain and the key
	serverCert, err := tls.X509KeyPair(certChain, key)
	if err != nil {
		return nil, err
	}

	return &serverCert, nil
}

// GenerateAndWriteNewCSR generates a new pub/priv key pair, writes it to disk
// and returns the CSR and the private key material
func GenerateAndWriteNewCSR(paths CertPaths) (csr, key []byte, err error) {
	// Generate a new key pair
	csr, key, err = generateNewCSR()
	if err != nil {
		return
	}

	// Ensure directory exists
	err = os.MkdirAll(filepath.Dir(paths.CSR), 0755)
	if err != nil {
		return
	}

	// Write CSR and key to disk
	if err = ioutil.WriteFile(paths.CSR, csr, 0644); err != nil {
		return
	}
	if err = ioutil.WriteFile(paths.Key, key, 0600); err != nil {
		return
	}

	return
}

// GetRemoteSignedCertificate submits a CSR together with the intended role to a remote CA server address
// available through a picker, and that is part of a CA identified by a specific certificate pool.
func GetRemoteSignedCertificate(ctx context.Context, csr []byte, role string, rootCAPool *x509.CertPool, picker *picker.Picker) ([]byte, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}
	if picker == nil {
		return nil, fmt.Errorf("valid remote address picker required")
	}

	// This is our only non-MTLS request
	// We're using CARole as server name, so an external CA doesn't also have to have ManagerRole in the cert SANs
	creds := credentials.NewTLS(&tls.Config{ServerName: CARole, RootCAs: rootCAPool})
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithBackoffMaxDelay(10 * time.Second),
		grpc.WithPicker(picker)}

	firstAddr, err := picker.PickAddr()
	if err != nil {
		return nil, err
	}

	conn, err := grpc.Dial(firstAddr, opts...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	// Create a CAClient to retreive a new Certificate
	caClient := api.NewCAClient(conn)

	// Send the Request and retrieve the request token
	issueRequest := &api.IssueCertificateRequest{CSR: csr, Role: role}
	issueResponse, err := caClient.IssueCertificate(ctx, issueRequest)
	if err != nil {
		return nil, err
	}

	token := issueResponse.Token

	statusRequest := &api.CertificateStatusRequest{Token: token}
	expBackoff := events.NewExponentialBackoff(events.ExponentialBackoffConfig{
		Base:   time.Second,
		Factor: time.Second,
		Max:    30 * time.Second,
	})

	log.Infof("Waiting for TLS certificate to be issued...")
	// Exponential backoff with Max of 30 seconds to wait for a new retry
	for {
		// Send the Request and retrieve the certificate
		statusReponse, err := caClient.CertificateStatus(ctx, statusRequest)
		if err != nil {
			return nil, err
		}

		// If the certificate was issued, return
		if statusReponse.Status.State == api.IssuanceStateIssued {
			return statusReponse.RegisteredCertificate.Certificate, nil
		}

		// If the certificate has been rejected or blocked return with an error
		retryStates := map[api.IssuanceState]bool{
			api.IssuanceStateRejected: true,
			api.IssuanceStateBlocked:  true,
		}
		if retryStates[statusReponse.Status.State] {
			return nil, fmt.Errorf("certificate issuance rejected: %v", statusReponse.Status.State)
		}

		// If we're still pending, the issuance failed, or the state is unknown
		// let's continue trying.
		expBackoff.Failure(nil, nil)
		time.Sleep(expBackoff.Proceed(nil))
	}
}

func saveRootCA(rootCA RootCA, paths CertPaths) error {
	// Make sure the necessary dirs exist and they are writable
	err := os.MkdirAll(filepath.Dir(paths.Cert), 0755)
	if err != nil {
		return err
	}

	// If the root certificate got returned successfully, save the rootCA to disk.
	return ioutil.WriteFile(paths.Cert, rootCA.Cert, 0644)
}

func generateNewCSR() (csr, key []byte, err error) {
	req := &cfcsr.CertificateRequest{
		KeyRequest: cfcsr.NewBasicKeyRequest(),
	}

	csr, key, err = cfcsr.ParseRequest(req)
	if err != nil {
		log.Debugf(`failed to generate CSR`)
		return
	}

	return
}
