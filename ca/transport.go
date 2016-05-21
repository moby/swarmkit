package ca

import (
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"google.golang.org/grpc/credentials"

	"golang.org/x/net/context"
)

var (
	// alpnProtoStr are the specified application level protocols for gRPC.
	alpnProtoStr = []string{"h2"}
)

// MutableTransportAuthenticator is an interface that wraps TransportAuthenticator
// but allows loading new TLS configs on the fly.
type MutableTransportAuthenticator interface {
	credentials.TransportAuthenticator

	LoadNewTLSConfig(config *tls.Config) error
	Role() string
	NodeID() string
}

type timeoutError struct{}

func (timeoutError) Error() string   { return "credentials: Dial timed out" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

// mutableTLSCreds is the credentials required for authenticating a connection using TLS.
type mutableTLSCreds struct {
	// Mutex for the tls config
	sync.Mutex
	// TLS configuration
	config *tls.Config
	// TLS Credentials
	tlsCreds credentials.TransportAuthenticator
	// store the subject for easy access
	subject pkix.Name
}

// Info implements the credentials.TransportAuthenticator interface
func (c *mutableTLSCreds) Info() credentials.ProtocolInfo {
	return c.tlsCreds.Info()
}

// GetRequestMetadata implements the credentials.TransportAuthenticator interface
func (c *mutableTLSCreds) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return c.tlsCreds.GetRequestMetadata(ctx, uri...)
}

// RequireTransportSecurity implements the credentials.TransportAuthenticator interface
func (c *mutableTLSCreds) RequireTransportSecurity() bool {
	return c.tlsCreds.RequireTransportSecurity()
}

// ClientHandshake implements the credentials.TransportAuthenticator interface
func (c *mutableTLSCreds) ClientHandshake(addr string, rawConn net.Conn, timeout time.Duration) (_ net.Conn, _ credentials.AuthInfo, err error) {
	c.Lock()
	defer c.Unlock()

	// borrow all the code from the original TLS credentials
	var errChannel chan error
	if timeout != 0 {
		errChannel = make(chan error, 2)
		time.AfterFunc(timeout, func() {
			errChannel <- timeoutError{}
		})
	}
	if c.config.ServerName == "" {
		colonPos := strings.LastIndex(addr, ":")
		if colonPos == -1 {
			colonPos = len(addr)
		}
		c.config.ServerName = addr[:colonPos]
	}
	conn := tls.Client(rawConn, c.config)
	if timeout == 0 {
		err = conn.Handshake()
	} else {
		go func() {
			errChannel <- conn.Handshake()
		}()
		err = <-errChannel
	}
	if err != nil {
		rawConn.Close()
		return nil, nil, err
	}

	return conn, nil, nil
}

// ServerHandshake implements the credentials.TransportAuthenticator interface
func (c *mutableTLSCreds) ServerHandshake(rawConn net.Conn) (net.Conn, credentials.AuthInfo, error) {
	c.Lock()
	defer c.Unlock()

	conn := tls.Server(rawConn, c.config)
	if err := conn.Handshake(); err != nil {
		rawConn.Close()
		return nil, nil, err
	}

	return conn, credentials.TLSInfo{State: conn.ConnectionState()}, nil
}

// LoadNewTLSConfig replaces the currently loaded TLS config with a new one
func (c *mutableTLSCreds) LoadNewTLSConfig(newConfig *tls.Config) error {
	c.Lock()
	defer c.Unlock()

	newSubject, err := getAndValidateCertificateSubject(newConfig.Certificates)
	if err != nil {
		return err
	}

	c.subject = newSubject
	c.config = newConfig

	return nil
}

// Role returns the OU for the certificate encapsulated in this TransportAuthenticator
func (c *mutableTLSCreds) Role() string {
	c.Lock()
	defer c.Unlock()

	return c.subject.OrganizationalUnit[0]
}

// NodeID returns the CN for the certificate encapsulated in this TransportAuthenticator
func (c *mutableTLSCreds) NodeID() string {
	c.Lock()
	defer c.Unlock()

	return c.subject.CommonName
}

// NewMutableTLS uses c to construct a mutable TransportAuthenticator based on TLS.
func NewMutableTLS(c *tls.Config) (MutableTransportAuthenticator, error) {
	originalTC := credentials.NewTLS(c)

	if len(c.Certificates) < 1 {
		return nil, fmt.Errorf("invalid configuration: needs at least one certificate")
	}

	subject, err := getAndValidateCertificateSubject(c.Certificates)
	if err != nil {
		return nil, err
	}

	tc := &mutableTLSCreds{config: c, tlsCreds: originalTC, subject: subject}
	tc.config.NextProtos = alpnProtoStr

	return tc, nil
}

// getCertificateSubject is a helper method to retrieve and validate the subject
// from the x509 certificate underlying a tls.Certificate
func getAndValidateCertificateSubject(certs []tls.Certificate) (pkix.Name, error) {
	for i := range certs {
		cert := &certs[i]
		x509Cert, err := x509.ParseCertificate(cert.Certificate[0])
		if err != nil {
			continue
		}
		if len(x509Cert.Subject.OrganizationalUnit) > 0 &&
			x509Cert.Subject.CommonName != "" {
			return x509Cert.Subject, nil
		}
	}

	return pkix.Name{}, fmt.Errorf("no valid subject names found for TLS configuration")
}
