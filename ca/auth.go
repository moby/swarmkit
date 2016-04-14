package ca

import (
	"crypto/tls"
	"crypto/x509/pkix"
	"fmt"
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

// GetCertificateUser extract the username from a client certificate.
func GetCertificateUser(tlsState *tls.ConnectionState) (pkix.Name, error) {
	if tlsState == nil {
		return pkix.Name{}, fmt.Errorf("request is not using TLS")
	}
	if len(tlsState.PeerCertificates) == 0 {
		return pkix.Name{}, fmt.Errorf("no client certificates in request")
	}
	if len(tlsState.VerifiedChains) == 0 {
		return pkix.Name{}, fmt.Errorf("no verified chains for remote certificate")
	}

	return tlsState.VerifiedChains[0][0].Subject, nil
}

// AuthorizeRole takes in a context and a list of organizations, and returns
// the CN of the certificate if one of the OU matches.
func AuthorizeRole(ctx context.Context, ou []string) (string, error) {
	if peer, ok := peer.FromContext(ctx); ok {
		if tlsInfo, ok := peer.AuthInfo.(credentials.TLSInfo); ok {
			certName, err := GetCertificateUser(&tlsInfo.State)
			if err != nil {
				return "", err
			}

			// Check if the current certificate has an OU that authorizes
			// access to this method
			if intersectArrays(certName.OrganizationalUnit, ou) {
				LogTLSState(ctx, &tlsInfo.State)
				return certName.CommonName, nil
			}

			return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: remote certificate not part of OU %v", ou)
		}
	}

	return "", grpc.Errorf(codes.PermissionDenied, "Permission denied: peer didn't not present valid peer certificate")
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
