package ca

import (
	log "github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
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

	randomID := identity.NewID()
	// TODO(diogo): Add pending for node types other than "agent"
	cert, err := ParseValidateAndSignCSR(s.securityConfig.Signer, request.CSR, randomID, request.NodeType)
	if err != nil {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	log.Debugf("(*CA).IssueCertificate: Issued certificate for CN=%s and OU=%s", randomID, request.NodeType)

	return &api.IssueCertificateResponse{
		Status:           &api.IssuanceStatus{Status: api.IssuanceStatusComplete},
		CertificateChain: cert,
	}, nil
}
