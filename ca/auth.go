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

// AuthorizeRole takes in a context and a list of roles, and returns
// the Node ID of the node.
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

// AuthorizeForwardedRole checks for proper roles of caller. The RPC may have
// been proxied by a manager, in which case the manager is authenticated and
// so is the certificate information that it forwarded. It returns the node ID
// of the original client.
func AuthorizeForwardedRole(ctx context.Context, authorizedRoles, forwarderRoles []string) (string, error) {
	if isForwardedRequest(ctx) {
		_, err := AuthorizeRole(ctx, forwarderRoles)
		if err != nil {
			return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: unauthorized forwarder role, expecting: %v", forwarderRoles)
		}

		// This was a forwarded request. Authorize the forwarder, and
		// check if the forwarded role matches one of the authorized
		// roles.
		forwardedID, forwardedOUs := forwardedTLSInfoFromContext(ctx)

		if len(forwardedOUs) == 0 || forwardedID == "" {
			return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: missing information in forwarded request")
		}

		if !intersectArrays(forwardedOUs, authorizedRoles) {
			return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: unauthorized forwarded role, expecting: %v", authorizedRoles)
		}

		return forwardedID, nil
	}

	// There wasn't any node being forwarded, check if this is a direct call by the expected role
	nodeID, err := AuthorizeRole(ctx, authorizedRoles)
	if err == nil {
		return nodeID, nil
	}

	return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: unauthorized peer role, expecting: %v", authorizedRoles)
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

// RemoteNodeInfo describes a node sending an RPC request.
type RemoteNodeInfo struct {
	// Roles is a list of roles contained in the node's certificate
	// (or forwarded by a trusted node).
	Roles []string

	// NodeID is the node's ID, from the CN field in its certificate
	// (or forwarded by a trusted node).
	NodeID string

	// ForwardedBy contains information for the node that forwarded this
	// request. It is set to nil if the request was received directly.
	ForwardedBy *RemoteNodeInfo
}

// RemoteNode returns the node ID and role from the client's TLS certificate.
// If the RPC was forwarded, the original client's ID and role is returned, as
// well as the forwarder's ID. This function does not do authorization checks -
// it only looks up the node ID.
func RemoteNode(ctx context.Context) (RemoteNodeInfo, error) {
	certSubj, err := certSubjectFromContext(ctx)
	if err != nil {
		return RemoteNodeInfo{}, err
	}

	directInfo := RemoteNodeInfo{
		Roles:  certSubj.OrganizationalUnit,
		NodeID: certSubj.CommonName,
	}

	if isForwardedRequest(ctx) {
		cn, ou := forwardedTLSInfoFromContext(ctx)
		if len(ou) == 0 || cn == "" {
			return RemoteNodeInfo{}, grpc.Errorf(codes.PermissionDenied, "Permission denied: missing information in forwarded request")
		}
		return RemoteNodeInfo{Roles: ou, NodeID: cn, ForwardedBy: &directInfo}, nil
	}

	return directInfo, nil
}
