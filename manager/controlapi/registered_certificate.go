package controlapi

import (
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

func validateRegisteredCertificateSpec(spec *api.RegisteredCertificateSpec) error {
	if spec == nil {
		return grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}
	return nil
}

// GetRegisteredCertificate returns a RegisteredCertificate given a RegisteredCertificateID.
// - Returns `InvalidArgument` if RegisteredCertificateID is not provided.
// - Returns `NotFound` if the RegisteredCertificateID is not found.
func (s *Server) GetRegisteredCertificate(ctx context.Context, request *api.GetRegisteredCertificateRequest) (*api.GetRegisteredCertificateResponse, error) {
	if request.RegisteredCertificateID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, errInvalidArgument.Error())
	}

	var rCert *api.RegisteredCertificate
	s.store.View(func(tx store.ReadTx) {
		rCert = store.GetRegisteredCertificate(tx, request.RegisteredCertificateID)
	})
	if rCert == nil {
		return nil, grpc.Errorf(codes.NotFound, "registered certificate %s not found", request.RegisteredCertificateID)
	}
	return &api.GetRegisteredCertificateResponse{
		RegisteredCertificate: rCert,
	}, nil
}

// ListRegisteredCertificates returns known certificates, optionally filtering by
// issuance state.
func (s *Server) ListRegisteredCertificates(ctx context.Context, request *api.ListRegisteredCertificatesRequest) (*api.ListRegisteredCertificatesResponse, error) {
	var (
		rCertificates []*api.RegisteredCertificate
		err           error
	)
	s.store.View(func(tx store.ReadTx) {
		if len(request.State) == 0 {
			rCertificates, err = store.FindRegisteredCertificates(tx, store.All)
			return
		}
		for _, issuanceState := range request.State {
			var certsByState []*api.RegisteredCertificate
			certsByState, err = store.FindRegisteredCertificates(tx, store.ByIssuanceState(issuanceState))
			if err != nil {
				return
			}
			rCertificates = append(rCertificates, certsByState...)
		}
	})
	if err != nil {
		return nil, grpc.Errorf(codes.Internal, codes.Internal.String())
	}

	return &api.ListRegisteredCertificatesResponse{
		Certificates: rCertificates,
	}, nil
}

// UpdateRegisteredCertificate updates a registered certificate.
// - Returns `InvalidArgument` if no certificate is specified.
// - Returns `NotFound` if the certificate is not found in the store.
func (s *Server) UpdateRegisteredCertificate(ctx context.Context, request *api.UpdateRegisteredCertificateRequest) (*api.UpdateRegisteredCertificateResponse, error) {
	if request.RegisteredCertificateID == "" || request.RegisteredCertificateVersion == nil {
		return nil, grpc.Errorf(codes.InvalidArgument, "no certificate ID specified")
	}

	if err := validateRegisteredCertificateSpec(request.Spec); err != nil {
		return nil, err
	}

	var rCert *api.RegisteredCertificate
	err := s.store.Update(func(tx store.Tx) error {
		rCert = store.GetRegisteredCertificate(tx, request.RegisteredCertificateID)
		if rCert == nil {
			return nil
		}
		rCert.Meta.Version = *request.RegisteredCertificateVersion
		rCert.Spec = *request.Spec.Copy()
		return store.UpdateRegisteredCertificate(tx, rCert)
	})
	if err != nil {
		return nil, err
	}
	if rCert == nil {
		return nil, grpc.Errorf(codes.NotFound, "registered certificate %s not found", request.RegisteredCertificateID)
	}
	return &api.UpdateRegisteredCertificateResponse{
		RegisteredCertificate: rCert,
	}, nil
}
