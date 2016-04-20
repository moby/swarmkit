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
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
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

// GetRootCA validates if the contents of certFile are a valid self-signed
// CA certificate, and returns the PEM-encoded Certificate if so
func GetRootCA(certFile string) ([]byte, error) {
	// Check if we have a Certificate file
	caPem, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}

	// Check to see if the Certificate file is a valid, self-signed Cert
	_, err = helpers.ParseSelfSignedCertificatePEM(caPem)
	if err != nil {
		return nil, err
	}

	// TODO(diogo): check expiration

	return caPem, nil
}

// CreateRootCA creates a Certificate authority for a new Swarm Cluster, potentially
// overwriting any existing CAs.
func CreateRootCA(pathToCert, pathToKey, rootCN string) (signer.Signer, []byte, error) {
	// Create a simple CSR for the CA using the default CA validator and policy
	log.Debugf("generating a new CA in: %s with CN=%s, using a %dbit %s key.", pathToCert, rootCN, RootKeySize, RootKeyAlgo)
	req := cfcsr.CertificateRequest{
		CN:         rootCN,
		KeyRequest: cfcsr.NewBasicKeyRequest(),
	}

	// Generate the CA and get the certificate and private key
	cert, _, key, err := initca.New(&req)
	if err != nil {
		return nil, nil, err
	}

	// Convert the key given by initca to an object to create a signer
	parsedKey, err := helpers.ParsePrivateKeyPEM(key)
	if err != nil {
		log.Errorf("failed to parse private key: %v", err)
		return nil, nil, err
	}

	// Convert the certificate into an object to create a signer
	parsedCert, err := helpers.ParseCertificatePEM(cert)
	if err != nil {
		return nil, nil, err
	}

	// Create a Signer out of the private key
	signer, err := local.NewSigner(parsedKey, parsedCert, signer.DefaultSigAlgo(parsedKey), DefaultPolicy())
	if err != nil {
		log.Errorf("failed to create signer: %v", err)
		return nil, nil, err
	}

	// Write the Private Key and Certificate to disk, using decent permissions
	if err := ioutil.WriteFile(pathToCert, cert, 0644); err != nil {
		return nil, nil, err
	}
	if err := ioutil.WriteFile(pathToKey, key, 0600); err != nil {
		return nil, nil, err
	}

	return signer, cert, nil
}

// GenerateAndSignNewTLSCert creates a new keypair, signs the certificate using signer,
// and saves the certificate and key to disk. This method is used to bootstrap the first
// manager TLS certificates.
func GenerateAndSignNewTLSCert(caSigner signer.Signer, rootCACert []byte, pathToCert, pathToKey, cn, ou string) (*tls.Certificate, error) {
	// Generate and new keypair and CSR
	csr, key, err := generateNewCSR()
	if err != nil {
		return nil, err
	}

	// Obtain a signed Certificate
	cert, err := ParseValidateAndSignCSR(caSigner, csr, cn, ou)
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	// Append the root CA Key to the certificate, to create a valid chain
	certChain := append(cert, rootCACert...)

	// Write both the chain and key to disk
	if err := ioutil.WriteFile(pathToCert, certChain, 0644); err != nil {
		return nil, err
	}
	if err := ioutil.WriteFile(pathToKey, key, 0600); err != nil {
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
func GenerateAndWriteNewCSR(pathToCSR, pathToKey string) (csr, key []byte, err error) {
	// Generate a new key pair
	csr, key, err = generateNewCSR()
	if err != nil {
		return
	}

	// Write CSR and key to disk
	if err = ioutil.WriteFile(pathToCSR, csr, 0644); err != nil {
		return
	}
	if err = ioutil.WriteFile(pathToKey, key, 0600); err != nil {
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
func ParseValidateAndSignCSR(caSigner signer.Signer, csrBytes []byte, cn, role string) ([]byte, error) {
	cert, err := caSigner.Sign(signer.SignRequest{
		Request: string(csrBytes),
		// OU is used for Authentication of the node type. The CN has the random
		// node ID.
		Subject: &signer.Subject{CN: cn, Names: []cfcsr.Name{{OU: role}}},
		// Adding role as DNS alt name, so clients can connect to "manager"
		Hosts: []string{role},
	})
	if err != nil {
		log.Debugf("failed to sign node certificate: %v", err)
		return nil, err
	}

	return cert, nil
}

// GetRemoteCA returns the remote endpoint's CA certificate, assuming the server
// is server the whole chain.
func GetRemoteCA(ctx context.Context, managerAddr, hashStr string) ([]byte, error) {
	// This TLS Config is intentionally using InsecureSkipVerify. Either we're
	// doing TOFU, in which case we don't validate the remote CA, or we're using
	// a user supplied hash to check the integrity of the CA certificate.
	insecureCreds := credentials.NewTLS(&tls.Config{InsecureSkipVerify: true})
	opts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second),
		grpc.WithTransportCredentials(insecureCreds)}

	conn, err := grpc.Dial(managerAddr, opts...)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	client := api.NewCAClient(conn)
	response, err := client.GetRootCACertificate(ctx, &api.GetRootCACertificateRequest{})
	if err != nil {
		return nil, err
	}

	if hashStr != "" {
		shaHash := sha256.New()
		shaHash.Write(response.Certificate)
		md := shaHash.Sum(nil)
		mdStr := hex.EncodeToString(md)
		if hashStr != mdStr {
			return nil, fmt.Errorf("remote CA does not match fingerprint. Expected: %s, got %s", hashStr, mdStr)
		}
	}

	return response.Certificate, nil
}

func getSignedCertificate(ctx context.Context, csr []byte, rootCAPool *x509.CertPool, role, managerAddr string) ([]byte, error) {
	if rootCAPool == nil {
		return nil, fmt.Errorf("valid root CA pool required")
	}

	// This is our only non-MTLS request
	creds := credentials.NewTLS(&tls.Config{ServerName: ManagerRole, RootCAs: rootCAPool})
	opts := []grpc.DialOption{grpc.WithTimeout(10 * time.Second), grpc.WithTransportCredentials(creds)}

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
