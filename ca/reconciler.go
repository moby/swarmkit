// RootReconciler is a generic reconciliation loop for rotating a root CA object, and forcing all entities
// that hold certificates issued by the root CA, to change certificates.  This can be used for both the swarm
// cluster CA root rotation as well as generic PKI rotation.

package ca

import (
	"context"
	"reflect"
	"sync"
	"time"

	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/pkg/errors"
)

// ErrRootRotationChanged is to be returned by FinishRotation if the CA-to-be-rotated's target root for
// rotation has changed in the middle of another root rotation.
var ErrRootRotationChanged = errors.New("target root rotation has changed")

// A RootRotator encapsulates an interface for read/update operations on a collection of TLSTerminators,
// as well as the CA root itself, whose certs will be rotated by a RotationReconciler.
type RootRotator interface {
	// GetAll returns all TLSTerminators who are supposed to hold certificates issued by the same CA
	GetAllTerminators() ([]TLSTerminator, error)

	// RotateCerts takes a list of TLSTerminators whose certs need to be rotated, and rotates them.
	RotateCerts([]TLSTerminator) error

	// FinishRotation is a callback that will let the caller know when root rotation is done.  It is called
	// with the old root CA that is being rotated (so we make sure , as well as the new root CA.
	FinishRotation(*api.RootCA, *api.RootCA) error
}

// EntityID allows for identifying a TLSTerminator
type EntityID struct {
	ID   string
	Type string
}

// TLSTerminator is something that terminates a TLS connection (holds a certificate-key pair and uses it directly for
// TLS communication).
type TLSTerminator interface {
	// TrustRoot returns the trust certificate(s) as PEM bytes
	TrustRoot() []byte

	// IssuerInfo returns the issuer for the leaf certificate held by the TLSTerminator
	IssuerInfo() *IssuerInfo

	// Identifier returns a unique identifier for the TLSTerminator
	Identifier() EntityID
}

// IssuerFromAPIRootCA returns the desired issuer given an API root CA object
func IssuerFromAPIRootCA(rootCA *api.RootCA) (*IssuerInfo, error) {
	wantedIssuer := rootCA.CACert
	if rootCA.RootRotation != nil {
		wantedIssuer = rootCA.RootRotation.CACert
	}
	issuerCerts, err := helpers.ParseCertificatesPEM(wantedIssuer)
	if err != nil {
		return nil, errors.Wrap(err, "invalid certificate in cluster root CA object")
	}
	if len(issuerCerts) == 0 {
		return nil, errors.New("invalid certificate in cluster root CA object")
	}
	return &IssuerInfo{
		Subject:   issuerCerts[0].RawSubject,
		PublicKey: issuerCerts[0].RawSubjectPublicKeyInfo,
	}, nil
}

// RotationReconciler keeps track of entities that use TLS certificates so that we can determine which ones need
// reconciliation when they are updated or the root CA that issues the certificates are updated.  This object
// provides functions to be called whenever the root CA changes or when an entity is added, updated, or removed.
type RotationReconciler struct {
	mu                sync.Mutex
	rotator           RootRotator
	reconcileInterval time.Duration
	ctx               context.Context

	currentRootCA *api.RootCA
	currentIssuer IssuerInfo

	// unconverged certs keeps a mapping of the objects which still need their leaf certificates rotated
	unconvergedCerts map[EntityID]TLSTerminator

	wg     sync.WaitGroup
	cancel func()
}

// NewRotationReconciler constructs a rotation reconciler given a root rotator and other parameters.
func NewRotationReconciler(ctx context.Context, r RootRotator, reconcileInterval time.Duration) *RotationReconciler {
	return &RotationReconciler{
		rotator:           r,
		ctx:               ctx,
		reconcileInterval: reconcileInterval,
	}
}

// UpdateRootCA is meant to get called whenever the root CA is updated, so that the RotationReconciler
func (r *RotationReconciler) UpdateRootCA(newRootCA *api.RootCA) error {
	if newRootCA == nil {
		return errors.New("root CA is nil")
	}
	issuerInfo, err := IssuerFromAPIRootCA(newRootCA)
	if err != nil {
		return errors.Wrap(err, "unable to update process the new root CA")
	}

	var (
		shouldStartNewLoop, waitForPrevLoop bool
		loopCtx                             context.Context
	)
	r.mu.Lock()
	defer func() {
		r.mu.Unlock()
		if shouldStartNewLoop {
			if waitForPrevLoop {
				r.wg.Wait()
			}
			r.wg.Add(1)
			go r.runReconcilerLoop(loopCtx, newRootCA)
		}
	}()

	// check if the issuer has changed.  If it has, then we don't need to update anything.
	if reflect.DeepEqual(&r.currentIssuer, issuerInfo) {
		r.currentRootCA = newRootCA
		return nil
	}
	// If the issuer has changed, iterate through all the nodes to figure out which ones need rotation
	if newRootCA.RootRotation != nil {
		tts, err := r.rotator.GetAllTerminators()
		if err != nil {
			return err
		}

		// from here on out, there will be no more errors that cause us to have to abandon updating the Root CA,
		// so we can start making changes to r's fields
		r.unconvergedCerts = make(map[EntityID]TLSTerminator)
		for _, tt := range tts {
			if !reflect.DeepEqual(tt.IssuerInfo(), issuerInfo) {
				r.unconvergedCerts[tt.Identifier()] = tt
			}
		}
		shouldStartNewLoop = true
		if r.cancel != nil { // there's already a loop going, so cancel it
			r.cancel()
			waitForPrevLoop = true
		}
		loopCtx, r.cancel = context.WithCancel(r.ctx)
	} else {
		r.unconvergedCerts = nil
	}
	r.currentRootCA = newRootCA
	r.currentIssuer = *issuerInfo
	return nil
}

// UpdateTLSTerminator is meant to get called whenever a TLSTerminator object is updated, so the reconciler can
// check its TLS certificate has the desired issuer.
func (r *RotationReconciler) UpdateTLSTerminator(tt TLSTerminator) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// if we're not in the middle of a root rotation ignore the update
	if r.currentRootCA == nil || r.currentRootCA.RootRotation == nil {
		return
	}
	if reflect.DeepEqual(tt.IssuerInfo(), &r.currentIssuer) {
		delete(r.unconvergedCerts, tt.Identifier())
	} else {
		r.unconvergedCerts[tt.Identifier()] = tt
	}
}

// DeleteTLSTerminator is meant to get called whenever a TLSTerminator object is removed/no longer relevant, so the
// reconciler can ignore it when determining whether a root rotation should bef inished.
func (r *RotationReconciler) DeleteTLSTerminator(tt TLSTerminator) {
	r.mu.Lock()
	delete(r.unconvergedCerts, tt.Identifier())
	r.mu.Unlock()
}

// every `reconcileInterval`, try to force a root rotation on TLSTerminators that have not been updated, or finish
// the root rotation if all TLSTerminators have desired certificates.
func (r *RotationReconciler) runReconcilerLoop(ctx context.Context, loopRootCA *api.RootCA) {
	defer r.wg.Done()
	for {
		r.mu.Lock()
		if len(r.unconvergedCerts) == 0 {
			r.mu.Unlock()
			err := r.rotator.FinishRotation(loopRootCA, &api.RootCA{
				CACert: loopRootCA.RootRotation.CACert,
				CAKey:  loopRootCA.RootRotation.CAKey,
			})
			if err == nil {
				log.G(r.ctx).Info("completed root rotation")
				return
			}
			log.G(r.ctx).WithError(err).Error("could not complete root rotation")
			if err == ErrRootRotationChanged {
				// if the root rotation has changed, this loop will be cancelled anyway, so may as well abort early
				return
			}
		} else {
			var toRotate []TLSTerminator
			for _, tt := range r.unconvergedCerts {
				toRotate = append(toRotate, tt)
			}
			r.mu.Unlock()
			r.rotator.RotateCerts(toRotate) // ignore any errors
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(r.reconcileInterval):
		}
	}
}
