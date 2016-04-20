package ca

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Server is the CA API gRPC server.
type Server struct {
	securityConfig *ManagerSecurityConfig
}

// NewServer creates a CA API server.
func NewServer(securityConfig *ManagerSecurityConfig) *Server {
	return &Server{
		securityConfig: securityConfig,
	}
}

// IssueCertificate receives requests from a remote client indicating a node type and a CSR,
// returning a certificate chain signed by the local CA, if available.
func (s *Server) IssueCertificate(ctx context.Context, request *api.IssueCertificateRequest) (*api.IssueCertificateResponse, error) {
	if request.CSR == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	// If this manager isn't a rootCA or an intermediate CA, we can't issue certificates
	if !s.securityConfig.RootCA && !s.securityConfig.IntCA {
		return nil, grpc.Errorf(codes.Unavailable, codes.Unavailable.String())
	}

	// Validate if this request is for a valid role
	if request.Role != ManagerRole && request.Role != AgentRole {
		return nil, grpc.Errorf(codes.InvalidArgument, "invalid role type requested")
	}

	// Generate a random ID for this new node
	randomID := identity.NewID()

	// TODO(diogo): Add pending for node types other than TypeAgent
	cert, err := ParseValidateAndSignCSR(s.securityConfig.Signer, request.CSR, randomID, request.Role)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	// Remote users are expecting a full certificate chain, not just a signed certificate
	certChain := append(cert, s.securityConfig.RootCACert...)

	log.G(ctx).Debugf("(*Server).IssueCertificate: Issued certificate for CN=%s and OU=%s", randomID, request.Role)

	return &api.IssueCertificateResponse{
		Status:           &api.IssuanceStatus{Status: api.IssuanceStatusComplete},
		CertificateChain: certChain,
	}, nil
}

// GetRootCACertificate returns the certificate of the Root CA.
func (s *Server) GetRootCACertificate(ctx context.Context, request *api.GetRootCACertificateRequest) (*api.GetRootCACertificateResponse, error) {

	log.G(ctx).Debugf("(*Server).GetRootCACertificate called ")

	return &api.GetRootCACertificateResponse{
		Certificate: s.securityConfig.RootCACert,
	}, nil
}
