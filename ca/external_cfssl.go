package ca

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"sync"

	log "github.com/Sirupsen/logrus"
	"github.com/cloudflare/cfssl/api"
	"github.com/cloudflare/cfssl/signer"
)

// ExternalCFSSLCA is able to make certificate signing requests to one of a list
// remote CFSSL API endpoints.
type ExternalCFSSLCA struct {
	mu     sync.RWMutex
	rootCA *RootCA
	url    string
	client *http.Client
}

// NewExternalCFSSLCA creates a new ExternalCA which uses the given tlsConfig to
// authenticate to the given CFSSL API endpoint.
func NewExternalCFSSLCA(rootCA *RootCA, tlsConfig *tls.Config, url string) ExternalCA {
	return &ExternalCFSSLCA{
		rootCA: rootCA,
		url:    url,
		client: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
	}
}

// UpdateTLSConfig updates the HTTP Client for this ExternalCA by creating
// a new client which uses the given tlsConfig.
func (eca *ExternalCFSSLCA) UpdateTLSConfig(tlsConfig *tls.Config) {
	eca.mu.Lock()
	defer eca.mu.Unlock()

	eca.client = &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: tlsConfig,
		},
	}
}

// Sign signs a new certificate by proxying the given certificate signing
// request to an external CFSSL API server.
func (eca *ExternalCFSSLCA) Sign(req signer.SignRequest) (cert []byte, err error) {
	// Get the current HTTP client and list of URL in a small critical
	// section. We will use these to make certificate signing requests.
	eca.mu.RLock()
	defer eca.mu.RUnlock()

	url := eca.url
	client := eca.client

	if url == "" {
		return nil, ErrNoExternalCAURL
	}

	csrJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("unable to JSON-encode CFSSL signing request: %s", err)
	}

	cert, err = makeExternalSignRequest(client, url, csrJSON)
	if err == nil {
		return eca.rootCA.AppendFirstRootPEM(cert)
	}

	log.Debugf("unable to proxy certificate signing request to %s: %s", url, err)

	return nil, err
}

func makeExternalSignRequest(client *http.Client, url string, csrJSON []byte) (cert []byte, err error) {
	resp, err := client.Post(url, "application/json", bytes.NewReader(csrJSON))
	if err != nil {
		return nil, fmt.Errorf("unable to perform certificate signing request: %s", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("unable to read CSR response body: %s", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code in CSR response: %d - %s", resp.StatusCode, string(body))
	}

	var apiResponse api.Response
	if err := json.Unmarshal(body, &apiResponse); err != nil {
		log.Debugf("unable to JSON-parse CFSSL API response body: %s", string(body))
		return nil, fmt.Errorf("unable to parse JSON response: %s", err)
	}

	if !apiResponse.Success || apiResponse.Result == nil {
		if len(apiResponse.Errors) > 0 {
			return nil, fmt.Errorf("response errors: %v", apiResponse.Errors)
		}

		return nil, fmt.Errorf("certificate signing request failed")
	}

	result, ok := apiResponse.Result.(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid result type: %T", apiResponse.Result)
	}

	certPEM, ok := result["certificate"].(string)
	if !ok {
		return nil, fmt.Errorf("invalid result certificate field type: %T", result["certificate"])
	}

	return []byte(certPEM), nil
}
