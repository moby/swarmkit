package testutils

import (
	"crypto/tls"
	"encoding/asn1"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"

	"encoding/hex"

	"github.com/cloudflare/cfssl/api"
	"github.com/cloudflare/cfssl/csr"
	cfsslerrors "github.com/cloudflare/cfssl/errors"
	"github.com/cloudflare/cfssl/helpers"
	"github.com/cloudflare/cfssl/signer"
	"github.com/cloudflare/cfssl/signer/local"
	"github.com/docker/swarmkit/ca"
	"github.com/pkg/errors"
)

// NewExternalSigningServer creates and runs a new ExternalSigningServer which
// uses the given rootCA to sign node certificates. A server key and cert are
// generated and saved into the given basedir and then a TLS listener is
// started on a random available port. On success, an HTTPS server will be
// running in a separate goroutine. The URL of the singing endpoint is
// available in the returned *ExternalSignerServer value. Calling the Close()
// method will stop the server.
func NewExternalSigningServer(rootCA ca.RootCA, basedir string) (*ExternalSigningServer, error) {
	serverCN := "external-ca-example-server"
	serverOU := "localhost" // Make a valid server cert for localhost.

	s, err := rootCA.Signer()
	if err != nil {
		return nil, err
	}

	// Create TLS credentials for the external CA server which we will run.
	serverPaths := ca.CertPaths{
		Cert: filepath.Join(basedir, "server.crt"),
		Key:  filepath.Join(basedir, "server.key"),
	}
	serverCert, err := rootCA.IssueAndSaveNewCertificates(ca.NewKeyReadWriter(serverPaths, nil, nil), serverCN, serverOU, "")
	if err != nil {
		return nil, errors.Wrap(err, "unable to get TLS server certificate")
	}

	serverTLSConfig := &tls.Config{
		Certificates: []tls.Certificate{*serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    rootCA.Pool,
	}

	tlsListener, err := tls.Listen("tcp", "localhost:0", serverTLSConfig)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create TLS connection listener")
	}

	assignedPort := tlsListener.Addr().(*net.TCPAddr).Port

	signURL := url.URL{
		Scheme: "https",
		Host:   net.JoinHostPort("localhost", strconv.Itoa(assignedPort)),
		Path:   "/sign",
	}

	ess := &ExternalSigningServer{
		listener: tlsListener,
		URL:      signURL.String(),
	}

	mux := http.NewServeMux()
	handler := &signHandler{
		numIssued:  &ess.NumIssued,
		leafSigner: s,
		flaky:      &ess.flaky,
	}
	mux.Handle(signURL.Path, handler)
	ess.handler = handler

	server := &http.Server{
		Handler: mux,
	}

	go server.Serve(tlsListener)

	return ess, nil
}

// ExternalSigningServer runs an HTTPS server with an endpoint at a specified
// URL which signs node certificate requests from a swarm manager client.
type ExternalSigningServer struct {
	listener  net.Listener
	NumIssued uint64
	URL       string
	flaky     uint32
	handler   *signHandler
}

// Stop stops this signing server by closing the underlying TCP/TLS listener.
func (ess *ExternalSigningServer) Stop() error {
	return ess.listener.Close()
}

// Flake makes the signing server return HTTP 500 errors.
func (ess *ExternalSigningServer) Flake() {
	atomic.StoreUint32(&ess.flaky, 1)
}

// Deflake restores normal operation after a call to Flake.
func (ess *ExternalSigningServer) Deflake() {
	atomic.StoreUint32(&ess.flaky, 0)
}

// EnableCASigning updates the root CA signer to be able to sign CAs
func (ess *ExternalSigningServer) EnableCASigning() error {
	ess.handler.mu.Lock()
	defer ess.handler.mu.Unlock()

	rootCert, err := helpers.ParseCertificatePEM(ess.handler.leafSigner.Cert)
	if err != nil {
		return errors.Wrap(err, "could not parse old CA certificate")
	}
	rootSigner, err := helpers.ParsePrivateKeyPEM(ess.handler.leafSigner.Key)
	if err != nil {
		return errors.Wrap(err, "could not parse old CA key")
	}

	// without the whitelist, we can't accept signing requests with CA extensions
	policy := ca.SigningPolicy(ca.DefaultNodeCertExpiration)
	if policy.Default.ExtensionWhitelist == nil {
		policy.Default.ExtensionWhitelist = make(map[string]bool)
	}
	policy.Default.ExtensionWhitelist[ca.BasicConstraintsOID.String()] = true
	policy.Default.Usage = append(policy.Default.Usage, "cert sign")

	caSigner, err := local.NewSigner(rootSigner, rootCert, signer.DefaultSigAlgo(rootSigner), policy)
	if err != nil {
		return errors.Wrap(err, "could not create CA signer")
	}

	ess.handler.caSigner = caSigner
	return nil
}

// DisableCASigning prevents the server from being able to sign CA certificates
func (ess *ExternalSigningServer) DisableCASigning() {
	ess.handler.mu.Lock()
	defer ess.handler.mu.Unlock()
	ess.handler.caSigner = nil
}

type signHandler struct {
	mu         sync.Mutex
	numIssued  *uint64
	flaky      *uint32
	leafSigner *ca.LocalSigner
	caSigner   signer.Signer
}

func (h *signHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if atomic.LoadUint32(h.flaky) == 1 {
		w.WriteHeader(http.StatusInternalServerError)
	}

	// Check client authentication via mutual TLS.
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		cfsslErr := cfsslerrors.New(cfsslerrors.APIClientError, cfsslerrors.AuthenticationFailure)
		errResponse := api.NewErrorResponse("must authenticate sign request with mutual TLS", cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	clientSub := r.TLS.PeerCertificates[0].Subject

	// The client certificate OU should be for a swarm manager.
	if len(clientSub.OrganizationalUnit) == 0 || clientSub.OrganizationalUnit[0] != ca.ManagerRole {
		cfsslErr := cfsslerrors.New(cfsslerrors.APIClientError, cfsslerrors.AuthenticationFailure)
		errResponse := api.NewErrorResponse(fmt.Sprintf("client certificate OU must be %q", ca.ManagerRole), cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	// The client certificate must have an Org.
	if len(clientSub.Organization) == 0 {
		cfsslErr := cfsslerrors.New(cfsslerrors.APIClientError, cfsslerrors.AuthenticationFailure)
		errResponse := api.NewErrorResponse("client certificate must have an Organization", cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}
	clientOrg := clientSub.Organization[0]

	// Decode the certificate signing request.
	var signReq signer.SignRequest
	if err := json.NewDecoder(r.Body).Decode(&signReq); err != nil {
		cfsslErr := cfsslerrors.New(cfsslerrors.APIClientError, cfsslerrors.JSONError)
		errResponse := api.NewErrorResponse(fmt.Sprintf("unable to decode sign request: %s", err), cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	// The signReq should have additional subject info.
	reqSub := signReq.Subject
	if reqSub == nil {
		cfsslErr := cfsslerrors.New(cfsslerrors.CSRError, cfsslerrors.BadRequest)
		errResponse := api.NewErrorResponse("sign request must contain a subject field", cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	var (
		isCA    bool
		certPEM []byte
		err     error
	)
	// is this a CA CSR?  If so, do we support CA signing?
	// based on cfssl/signer/signer.go's ParseCertificateRequest to tell from the extensions if it's a CA
	for _, ext := range signReq.Extensions {
		// Check the CSR for the X.509 BasicConstraints (RFC 5280, 4.2.1.9)
		// extension and append to template if necessary
		if asn1.ObjectIdentifier(ext.ID).Equal(ca.BasicConstraintsOID) {
			rawVal, err := hex.DecodeString(ext.Value)
			if err != nil {
				continue
			}
			var constraints csr.BasicConstraints
			rest, err := asn1.Unmarshal(rawVal, &constraints)
			if err != nil || len(rest) != 0 {
				// technically failure conditions, but these will actually be caught when signing the request
				continue
			}

			if isCA = constraints.IsCA; isCA {
				break
			}
		}
	}

	h.mu.Lock()
	if isCA && h.caSigner != nil {
		// Sign the requested CA certificate
		certPEM, err = h.caSigner.Sign(signReq)
		h.mu.Unlock()
	} else {
		h.mu.Unlock()
		// The client's Org should match the Org in the sign request subject.
		if len(reqSub.Name().Organization) == 0 || reqSub.Name().Organization[0] != clientOrg {
			cfsslErr := cfsslerrors.New(cfsslerrors.CSRError, cfsslerrors.BadRequest)
			errResponse := api.NewErrorResponse("sign request subject org does not match client certificate org", cfsslErr.ErrorCode)
			json.NewEncoder(w).Encode(errResponse)
			return
		}

		// Sign the requested leaf certificate.
		certPEM, err = h.leafSigner.Sign(signReq)
	}
	if err != nil {
		cfsslErr := cfsslerrors.New(cfsslerrors.APIClientError, cfsslerrors.ServerRequestFailed)
		errResponse := api.NewErrorResponse(fmt.Sprintf("unable to sign requested certificate: %s", err), cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	result := map[string]string{
		"certificate": string(certPEM),
	}

	// Increment the number of certs issued.
	atomic.AddUint64(h.numIssued, 1)

	// Return a successful JSON response.
	json.NewEncoder(w).Encode(api.NewSuccessResponse(result))
}
