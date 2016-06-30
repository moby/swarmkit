package ca

import (
	"crypto/tls"
	"errors"

	"github.com/cloudflare/cfssl/signer"
)

// ErrNoExternalCAs is an error used it indicate that no properly configured
// ExternalCA is available to which it can proxy certificate signing requests.
var ErrNoExternalCAs = errors.New("no external CAs configured")

// ErrNoExternalCAURL is an error used to indicate that a given external CA
// does not have a properly configured URL
var ErrNoExternalCAURL = errors.New("no URL found for external CA")

// ExternalCA is able to make certificate signing requests to one of a list
// remote CFSSL API endpoints.
type ExternalCA interface {
	UpdateTLSConfig(*tls.Config)
	Sign(signer.SignRequest) ([]byte, error)
}
