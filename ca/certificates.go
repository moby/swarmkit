package ca

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"fmt"
	"io/ioutil"
	"time"

	log "github.com/Sirupsen/logrus"
	cfcsr "github.com/cloudflare/cfssl/csr"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/initca"
	cflog "github.com/cloudflare/cfssl/log"
	cfsigner "github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/docker/go-events"
	"github.com/docker/swarm-v2/api"
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

// GetRootCA validates if the contents of the file are a valid self-signed
// CA certificate, and returns the PEM-encoded Certificate if so
func GetRootCA(paths CertPaths) (Signer, error) {
	// Check if we have a Certificate file
	rootCACert, err := ioutil.ReadFile(paths.Cert)
	if err != nil {
		return Signer{}, err
	}

	// Check to see if the Certificate file is a valid, self-signed Cert
	_, err = helpers.ParseSelfSignedCertificatePEM(rootCACert)
	if err != nil {
		return Signer{}, err
	}

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return Signer{}, fmt.Errorf("error while adding root CA cert to Cert Pool")
	}

	// If there is a root CA keypair, we're going to try getting a crypto signer from it
	cryptoSigner, err := local.NewSignerFromFile(paths.Cert, paths.Key, DefaultPolicy())
	if err == nil {
		log.Debugf("successfully loaded the signer for the Root CA: %s", paths.Cert)
	}

	return Signer{CryptoSigner: cryptoSigner, RootCACert: rootCACert, RootCAPool: rootCAPool}, nil
}

// CreateRootCA creates a Certificate authority for a new Swarm Cluster, potentially
// overwriting any existing CAs.
func CreateRootCA(rootCN string, paths CertPaths) (Signer, error) {
	// Create a simple CSR for the CA using the default CA validator and policy
	log.Debugf("generating a new CA in: %s with CN=%s, using a %dbit %s key.", paths.Cert, rootCN, RootKeySize, RootKeyAlgo)
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: cfcsr.NewBasicKeyRequest(),
		// CA:         &cfcsr.CAConfig{Expiry: "10y"},
	}

	// Generate the CA and get the certificate and private key
	rootCACert, _, key, err := initca.New(&req)
	if err != nil {
		return Signer{}, err
	}

	// Convert the key given by initca to an object to create a signer
	parsedKey, err := helpers.ParsePrivateKeyPEM(key)
	if err != nil {
		log.Errorf("failed to parse private key: %v", err)
		return Signer{}, err
	}

	// Convert the certificate into an object to create a signer
	parsedCert, err := helpers.ParseCertificatePEM(rootCACert)
	if err != nil {
		return Signer{}, err
	}

	// Create a Signer out of the private key
	signer, err := local.NewSigner(parsedKey, parsedCert, cfsigner.DefaultSigAlgo(parsedKey), DefaultPolicy())
	if err != nil {
		log.Errorf("failed to create signer: %v", err)
		return Signer{}, err
	}

	// Write the Private Key and Certificate to disk, using decent permissions
	if err := ioutil.WriteFile(paths.Cert, rootCACert, 0644); err != nil {
		return Signer{}, err
	}
	if err := ioutil.WriteFile(paths.Key, key, 0600); err != nil {
		return Signer{}, err
	}

	// Create a Pool with our RootCACertificate
	rootCAPool := x509.NewCertPool()
	if !rootCAPool.AppendCertsFromPEM(rootCACert) {
		return Signer{}, fmt.Errorf("failed to append certificate to cert pool")
	}

	return Signer{CryptoSigner: signer, RootCACert: rootCACert, RootCAPool: rootCAPool}, nil
}

// GenerateAndSignNewTLSCert creates a new keypair, signs the certificate using signer,
// and saves the certificate and key to disk. This method is used to bootstrap the first
// manager TLS certificates.
func GenerateAndSignNewTLSCert(signer Signer, cn, ou string, paths CertPaths) (*tls.Certificate, error) {
	// Generate and new keypair and CSR
	csr, key, err := generateNewCSR()
	if err != nil {
		return nil, err
	}

	// Obtain a signed Certificate
	cert, err := ParseValidateAndSignCSR(signer, csr, cn, ou)
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	// Append the root CA Key to the certificate, to create a valid chain
	certChain := append(cert, signer.RootCACert...)

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

	// Write CSR and key to disk
	if err = ioutil.WriteFile(paths.CSR, csr, 0644); err != nil {
		return
	}
	if err = ioutil.WriteFile(paths.Key, key, 0600); err != nil {
		return
	}

	return
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

// ParseValidateAndSignCSR returns a signed certificate from a particular signer and a CSR.
func ParseValidateAndSignCSR(signer Signer, csrBytes []byte, cn, role string) ([]byte, error) {
	// All managers get added the subject-alt-name of CA, so they can be used for cert issuance
	hosts := []string{role}
	if role == ManagerRole {
		hosts = append(hosts, CARole)
	}

	cert, err := signer.CryptoSigner.Sign(cfsigner.SignRequest{
		Request: string(csrBytes),
		// OU is used for Authentication of the node type. The CN has the random
		// node ID.
		Subject: &cfsigner.Subject{CN: cn, Names: []cfcsr.Name{{OU: role}}},
		// Adding role as DNS alt name, so clients can connect to "manager" and "ca"
		Hosts: hosts,
	})
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	return cert, nil
}

// GetRemoteCA returns the remote endpoint's CA certificate
func GetRemoteCA(ctx context.Context, managerAddr, hashStr string) (Signer, error) {
	// This TLS Config is intentionally using InsecureSkipVerify. Either we're
	// doing TOFU, in which case we don't validate the remote CA, or we're using
	// a user supplied hash to check the integrity of the CA certificate.
	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecureCreds)}

	conn, err := grpc.Dial(managerAddr, opts...)
	if err != nil {
		return Signer{}, err
	}
	defer conn.Close()

	client := api.NewCAClient(conn)
	response, err := client.GetRootCACertificate(ctx, &api.GetRootCACertificateRequest{})
	if err != nil {
		return Signer{}, err
	}

	if hashStr != "" {
		shaHash := sha256.New()
		shaHash.Write(response.Certificate)
		md := shaHash.Sum(nil)
		mdStr := hex.EncodeToString(md)
		if hashStr != mdStr {
			return Signer{}, fmt.Errorf("remote CA does not match fingerprint. Expected: %s, got %s", hashStr, mdStr)
		}
	}

	// Create a Pool with our RootCACertificate
	rootCACertPool := x509.NewCertPool()
	if !rootCACertPool.AppendCertsFromPEM(response.Certificate) {
		return Signer{}, fmt.Errorf("failed to append certificate to cert pool")
	}

	return Signer{RootCACert: response.Certificate, RootCAPool: rootCACertPool}, nil
}

func issueAndSaveNewCertificates(ctx context.Context, paths CertPaths, role, remoteAddr string, rootCAPool *x509.CertPool) (*tls.Certificate, error) {
	// Create a new key/pair and CSR for the new manager
	csr, key, err := GenerateAndWriteNewCSR(paths)
	if err != nil {
		log.Debugf("error when generating new node certs: %v", err)
		return nil, err
	}

	// Get the remote manager to issue a CA signed certificate for this node
	signedCert, err := getSignedCertificate(ctx, csr, role, remoteAddr, rootCAPool)
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

func getSignedCertificate(ctx context.Context, csr []byte, role, caAddr string, rootCAPool *x509.CertPool) ([]byte, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}

	// This is our only non-MTLS request
	creds := credentials.NewTLS(&tls.Config{ServerName: CARole, RootCAs: rootCAPool})
	opts := []grpc.DialOption{grpc.WithTransportCredentials(creds), grpc.WithBackoffMaxDelay(10 * time.Second)}

	// TODO(diogo): Add a connection picker
	conn, err := grpc.Dial(caAddr, opts...)
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
	var statusReponse *api.CertificateStatusResponse
	expBackoff := events.NewExponentialBackoff(events.ExponentialBackoffConfig{
		Base:   time.Second,
		Factor: time.Second,
		Max:    30 * time.Second,
	})

	// Exponential backoff with Max of 20 seconds to wait for the new certificate
	for {
		// Send the Request and retrieve the certificate
		statusReponse, err = caClient.CertificateStatus(ctx, statusRequest)
		if err != nil {
			return nil, err
		}

		// If the request is completed, we have a certificate to return
		if statusReponse.Status.State == api.IssuanceStateIssued {
			break
		}

		expBackoff.Failure(nil, nil)
		// Wait for next retry
		time.Sleep(expBackoff.Proceed(nil))
	}

	return statusReponse.RegisteredCertificate.Certificate, nil
}
