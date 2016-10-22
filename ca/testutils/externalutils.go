package testutils

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"sync/atomic"

	"github.com/cloudflare/cfssl/api"
	cfsslerrors "github.com/cloudflare/cfssl/errors"
	"github.com/cloudflare/cfssl/signer"
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
		numIssued: &ess.NumIssued,
		rootCA:    rootCA,
		flaky:     &ess.flaky,
	}
	mux.Handle(signURL.Path, handler)

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

type signHandler struct {
	numIssued *uint64
	rootCA    ca.RootCA
	flaky     *uint32
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

	// The client's Org should match the Org in the sign request subject.
	if len(reqSub.Name().Organization) == 0 || reqSub.Name().Organization[0] != clientOrg {
		cfsslErr := cfsslerrors.New(cfsslerrors.CSRError, cfsslerrors.BadRequest)
		errResponse := api.NewErrorResponse("sign request subject org does not match client certificate org", cfsslErr.ErrorCode)
		json.NewEncoder(w).Encode(errResponse)
		return
	}

	// Finally, sign the requested certificate.
	certPEM, err := h.rootCA.Signer.Sign(signReq)
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
