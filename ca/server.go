package ca

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Server is the CA API gRPC server.
type Server struct {
	store          state.WatchableStore
	securityConfig *ManagerSecurityConfig
}

// NewServer creates a CA API server.
func NewServer(store state.WatchableStore, securityConfig *ManagerSecurityConfig) *Server {
	return &Server{
		store:          store,
		securityConfig: securityConfig,
	}
}

// CertificateStatus returns the current issuance status of an issuance request identified by Token
func (s *Server) CertificateStatus(ctx context.Context, request *api.CertificateStatusRequest) (*api.CertificateStatusResponse, error) {
	if request.Token == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	var rCertificate *api.RegisteredCertificate
	s.store.View(func(tx state.ReadTx) {
		rCertificate = tx.RegisteredCertificates().Get(request.Token)
	})
	if rCertificate == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	// If this manager isn't a rootCA or an intermediate CA, we can't issue certificates
	// TODO(diogo): Issuance should only happen if we have a IssuanceStateAccepted.
	if rCertificate.Status.State == api.IssuanceStatePending &&
		(s.securityConfig.RootCA || s.securityConfig.IntCA) {

		// Generate a random ID for this new node
		randomID := identity.NewID()

		// TODO(diogo): Add pending for node types other than TypeAgent
		cert, err := ParseValidateAndSignCSR(s.securityConfig.Signer, rCertificate.CSR, randomID, rCertificate.Role)
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
		}

		// Remote users are expecting a full certificate chain, not just a signed certificate
		rCertificate.Certificate = append(cert, s.securityConfig.RootCACert...)
		rCertificate.CN = randomID
		rCertificate.Status = api.IssuanceStatus{
			State: api.IssuanceStateCompleted,
		}

		err = s.store.Update(func(tx state.Tx) error {
			latestCertificate := tx.RegisteredCertificates().Get(request.Token)
			if latestCertificate.Status.State == api.IssuanceStateCompleted {
				rCertificate = latestCertificate
				return nil
			}
			return tx.RegisteredCertificates().Update(rCertificate)
		})
		if err != nil {
			return nil, grpc.Errorf(codes.InvalidArgument, err.Error())
		}
		log.G(ctx).Debugf("(*Server).CertificateStatus: issued certificate for Node=%s and Role=%s", randomID, rCertificate.Role)
	}

	log.G(ctx).Debugf("(*Server).CertificateStatus: checking status for Token=%s, Status: %s", request.Token, rCertificate.Status)
	return &api.CertificateStatusResponse{
		Status:                &rCertificate.Status,
		RegisteredCertificate: rCertificate,
	}, nil

}

// IssueCertificate receives requests from a remote client indicating a node type and a CSR,
// returning a certificate chain signed by the local CA, if available.
func (s *Server) IssueCertificate(ctx context.Context, request *api.IssueCertificateRequest) (*api.IssueCertificateResponse, error) {
	if request.CSR == nil || request.Role == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	// Generate a random token for this new node
	token := identity.NewID()

	log.G(ctx).Debugf("(*Server).IssueCertificate: added issue certificate entry for Role=%s with Token=%s", request.Role, token)

	var certificate *api.RegisteredCertificate
	err := s.store.Update(func(tx state.Tx) error {
		certificate = &api.RegisteredCertificate{
			ID:   token,
			CSR:  request.CSR,
			Role: request.Role,
			Status: api.IssuanceStatus{
				State: api.IssuanceStatePending,
			},
		}
		return tx.RegisteredCertificates().Create(certificate)
	})
	if err != nil {
		return nil, err
	}

	return &api.IssueCertificateResponse{
		Token: token,
	}, nil
}

// GetRootCACertificate returns the certificate of the Root CA.
func (s *Server) GetRootCACertificate(ctx context.Context, request *api.GetRootCACertificateRequest) (*api.GetRootCACertificateResponse, error) {

	log.G(ctx).Debugf("(*Server).GetRootCACertificate called ")

	return &api.GetRootCACertificateResponse{
		Certificate: s.securityConfig.RootCACert,
	}, nil
}
