package ca

import (
	"crypto/tls"
	"crypto/x509/pkix"
	"strings"

	"github.com/Sirupsen/logrus"

	"github.com/docker/swarm-v2/log"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
)

// LogTLSState logs information about the TLS connection and remote peers
func LogTLSState(ctx context.Context, tlsState *tls.ConnectionState) {
	if tlsState == nil {
		log.G(ctx).Debugf("no TLS Chains found")
		return
	}

	peerCerts := []string{}
	verifiedChain := []string{}
	for _, cert := range tlsState.PeerCertificates {
		peerCerts = append(peerCerts, cert.Subject.CommonName)
	}
	for _, chain := range tlsState.VerifiedChains {
		subjects := []string{}
		for _, cert := range chain {
			subjects = append(subjects, cert.Subject.CommonName)
		}
		verifiedChain = append(verifiedChain, strings.Join(subjects, ","))
	}

	log.G(ctx).WithFields(logrus.Fields{
		"peer.peerCert": peerCerts,
		// "peer.verifiedChain": verifiedChain},
	}).Debugf("")
}

// getCertificateSubject extracts the subject from a verified client certificate
func getCertificateSubject(tlsState *tls.ConnectionState) (pkix.Name, error) {
	if tlsState == nil {
		return pkix.Name{}, grpc.Errorf(codes.PermissionDenied, "request is not using TLS")
	}
	if len(tlsState.PeerCertificates) == 0 {
		return pkix.Name{}, grpc.Errorf(codes.PermissionDenied, "no client certificates in request")
	}
	if len(tlsState.VerifiedChains) == 0 {
		return pkix.Name{}, grpc.Errorf(codes.PermissionDenied, "no verified chains for remote certificate")
	}

	return tlsState.VerifiedChains[0][0].Subject, nil
}

func tlsConnStateFromContext(ctx context.Context) (*tls.ConnectionState, error) {
	peer, ok := peer.FromContext(ctx)
	if !ok {
		return nil, grpc.Errorf(codes.PermissionDenied, "Permission denied: no peer info")
	}
	tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return nil, grpc.Errorf(codes.PermissionDenied, "Permission denied: peer didn't not present valid peer certificate")
	}
	return &tlsInfo.State, nil
}

// certSubjectFromContext extracts pkix.Name from context.
func certSubjectFromContext(ctx context.Context) (pkix.Name, error) {
	connState, err := tlsConnStateFromContext(ctx)
	if err != nil {
		return pkix.Name{}, err
	}
	return getCertificateSubject(connState)
}

// AuthorizeRole takes in a context and a list of organizations, and returns
// the CN of the certificate if one of the OU matches.
func AuthorizeRole(ctx context.Context, ou []string) (string, error) {
	certSubj, err := certSubjectFromContext(ctx)
	if err != nil {
		return "", err
	}
	// Check if the current certificate has an OU that authorizes
	// access to this method
	if intersectArrays(certSubj.OrganizationalUnit, ou) {
		return certSubj.CommonName, nil
	}
	return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: remote certificate not part of OU %v", ou)
}

// AuthorizeForwardedRole takes in a context and a list of organizations, and returns
// the CN of the certificate if one of the OU matches.
func AuthorizeForwardedRole(ctx context.Context, role string) (string, error) {
	return authorizeForwardedRole(ctx, role, []string{ManagerRole})
}

// authorizeForwardedRole checks for proper roles of caller. It can be manager who
// forward agent request or agent itself. It returns agent id.
func authorizeForwardedRole(ctx context.Context, forwardedRole string, forwarderRoles []string) (string, error) {
	// If the call is being done directly by an accepted role, return the CN
	cn, err := AuthorizeRole(ctx, []string{forwardedRole})
	if err == nil {
		return cn, nil
	}

	// If the call is being done by a manager, return the forwarded CN
	_, err = AuthorizeRole(ctx, forwarderRoles)
	if err == nil {
		return forwardCNFromContext(ctx)
	}

	return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: unknown peer role.")
}

// intersectArrays returns true when there is at least one element in common
// between the two arrays
func intersectArrays(orig, tgt []string) bool {
	for _, i := range orig {
		for _, x := range tgt {
			if i == x {
				return true
			}
		}
	}
	return false
}
