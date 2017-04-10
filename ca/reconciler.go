package ca

import (
	"context"
	"fmt"
	"sync"
	"time"

	"bytes"

	"reflect"

	"github.com/cloudflare/cfssl/helpers"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/api/equality"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
	"github.com/pkg/errors"
)

// IssuanceStateRotateBatchSize is the maximum number of nodes we'll tell to rotate their certificates in any given update
const IssuanceStateRotateBatchSize = 30

func hasIssuer(n *api.Node, info *IssuerInfo) bool {
	if n.Description == nil || n.Description.TLSInfo == nil {
		return false
	}
	return bytes.Equal(info.Subject, n.Description.TLSInfo.CertIssuerSubject) && bytes.Equal(info.PublicKey, n.Description.TLSInfo.CertIssuerPublicKey)
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
		return nil, errors.Wrap(err, "invalid certificate in cluster root CA object")
	}
	return &IssuerInfo{
		Subject:   issuerCerts[0].RawSubject,
		PublicKey: issuerCerts[0].RawSubjectPublicKeyInfo,
	}, nil
}

var errRootRotationChanged = errors.New("target root rotation has changed")

// rootRotationReconciler keeps track of all the nodes in the store so that we can determine which ones need reconciliation when nodes are updated
// or the root CA is updated.  This is meant to be used with watches on nodes and the cluster, and provides functions to be called when the
// cluster's RootCA has changed and when a node is added, updated, or removed.
type rootRotationReconciler struct {
	mu                  sync.Mutex
	clusterID           string
	batchUpdateInterval time.Duration
	ctx                 context.Context
	store               *store.MemoryStore

	currentRootCA    *api.RootCA
	currentIssuer    IssuerInfo
	allNodes         map[string]*api.Node
	unconvergedNodes map[string]struct{}

	wg     sync.WaitGroup
	cancel func()
}

func newReconciler(ctx context.Context, clusterID string, interval time.Duration, s *store.MemoryStore, rootCA *api.RootCA, nodes []*api.Node) *rootRotationReconciler {
	r := &rootRotationReconciler{
		ctx:                 ctx,
		clusterID:           clusterID,
		store:               s,
		batchUpdateInterval: interval,
		allNodes:            make(map[string]*api.Node),
		unconvergedNodes:    make(map[string]struct{}),
	}
	r.UpdateRootCA(rootCA)
	r.UpdateNodes(nodes...)
	return r
}

func (r *rootRotationReconciler) UpdateRootCA(newRootCA *api.RootCA) {
	if newRootCA == nil {
		return
	}
	issuerInfo, err := IssuerFromAPIRootCA(newRootCA)
	if err != nil {
		log.G(r.ctx).WithError(err).Error("unable to update process the current root CA")
	}
	r.mu.Lock()
	r.currentRootCA = newRootCA
	// check if the issuer has changed, first
	if reflect.DeepEqual(&r.currentIssuer, issuerInfo) {
		r.mu.Unlock()
		return
	}
	// If the issuer has changed, iterate through all the nodes to figure out which ones need rotation
	r.currentIssuer = *issuerInfo
	r.unconvergedNodes = make(map[string]struct{})
	var (
		hasRootRotation bool
		ctx             context.Context
		wg              *sync.WaitGroup
	)
	if r.currentRootCA.RootRotation != nil {
		hasRootRotation = true
		for _, n := range r.allNodes {
			if hasIssuer(n, &r.currentIssuer) {
				continue
			}
			r.unconvergedNodes[n.ID] = struct{}{}
		}
		if r.cancel != nil { // there's already a loop going, so cancel it
			r.cancel()
			wg = &r.wg
		}
		ctx, r.cancel = context.WithCancel(r.ctx)
	}
	r.mu.Unlock()

	if hasRootRotation {
		if wg != nil {
			wg.Wait()
		}
		go r.runReconcilerLoop(ctx, newRootCA)
	}
}

func (r *rootRotationReconciler) UpdateNodes(nodes ...*api.Node) {
	r.mu.Lock()
	for _, n := range nodes {
		if n == nil || n.Spec.Membership != api.NodeMembershipAccepted {
			continue
		}
		r.allNodes[n.ID] = n
		if r.currentRootCA == nil || r.currentRootCA.RootRotation == nil {
			continue
		}
		if hasIssuer(n, &r.currentIssuer) {
			delete(r.unconvergedNodes, n.ID)
		} else {
			r.unconvergedNodes[n.ID] = struct{}{}
		}
	}
	r.mu.Unlock()
}

func (r *rootRotationReconciler) DeleteNode(node *api.Node) {
	if node == nil {
		return
	}
	r.mu.Lock()
	delete(r.allNodes, node.ID)
	delete(r.unconvergedNodes, node.ID)
	r.mu.Unlock()
}

func (r *rootRotationReconciler) runReconcilerLoop(ctx context.Context, loopRootCA *api.RootCA) {
	r.wg.Add(1)
	defer r.wg.Done()
	for {
		r.mu.Lock()
		if len(r.unconvergedNodes) == 0 {
			r.mu.Unlock()

			err := r.store.Update(func(tx store.Tx) error {
				return r.finishRootRotation(tx, loopRootCA)
			})
			if err == nil {
				log.G(r.ctx).Infof("completed root rotation on cluster %s", r.clusterID)
				return
			}
			log.G(r.ctx).WithError(err).Errorf("could not complete root rotation")
			if err == errRootRotationChanged {
				// if the root rotation has changed, this loop will be cancelled anyway, so may as well abort early
				return
			}
		} else {
			var toUpdate []*api.Node
			for nodeID := range r.unconvergedNodes {
				n, ok := r.allNodes[nodeID]
				if !ok { // should never happen
					continue
				}
				iState := n.Certificate.Status.State
				if iState != api.IssuanceStateRenew&iState && iState != api.IssuanceStatePending && iState != api.IssuanceStateRotate {
					n = n.Copy()
					n.Certificate.Status.State = api.IssuanceStateRotate
					toUpdate = append(toUpdate, n)
					if len(toUpdate) >= IssuanceStateRotateBatchSize {
						break
					}
				}
			}
			r.mu.Unlock()

			if err := r.batchUpdateNodes(toUpdate); err != nil {
				log.G(r.ctx).WithError(err).Errorf("store error when trying to batch update %d nodes to request certificate rotation", len(toUpdate))
			}
		}

		select {
		case <-r.ctx.Done():
			return
		case <-time.After(r.batchUpdateInterval):
		}
	}
}

// This function assumes that the expected root CA has root rotation.  This is intended to be used by
// `reconcileNodeRootsAndCerts`, which uses the root CA from the `lastSeenClusterRootCA`, and checks
// that it has a root rotation before calling this function.
func (r *rootRotationReconciler) finishRootRotation(tx store.Tx, expectedRootCA *api.RootCA) error {
	cluster := store.GetCluster(tx, r.clusterID)
	if cluster == nil {
		return fmt.Errorf("unable to get cluster %s", r.clusterID)
	}

	// If the RootCA object has changed (because another root rotation was started or because some other node
	// had finished the root rotation), we cannot finish the root rotation that we were working on.
	if !equality.RootCAEqualStable(expectedRootCA, &cluster.RootCA) {
		return errRootRotationChanged
	}

	var signerCert []byte
	if len(cluster.RootCA.RootRotation.CAKey) > 0 {
		signerCert = cluster.RootCA.RootRotation.CACert
	}
	// we don't actually have to parse out the default node expiration from the cluster - we are just using
	// the ca.RootCA object to generate new tokens and the digest
	updatedRootCA, err := NewRootCA(cluster.RootCA.RootRotation.CACert, signerCert, cluster.RootCA.RootRotation.CAKey,
		DefaultNodeCertExpiration, nil)
	if err != nil {
		return errors.Wrap(err, "invalid cluster root rotation object")
	}
	cluster.RootCA = api.RootCA{
		CACert:     cluster.RootCA.RootRotation.CACert,
		CAKey:      cluster.RootCA.RootRotation.CAKey,
		CACertHash: updatedRootCA.Digest.String(),
		JoinTokens: api.JoinTokens{
			Worker:  GenerateJoinToken(&updatedRootCA),
			Manager: GenerateJoinToken(&updatedRootCA),
		},
		LastForcedRotation: cluster.RootCA.LastForcedRotation,
	}
	return store.UpdateCluster(tx, cluster)
}

func (r *rootRotationReconciler) batchUpdateNodes(toUpdate []*api.Node) error {
	if len(toUpdate) == 0 {
		return nil
	}
	_, err := r.store.Batch(func(batch *store.Batch) error {
		// Directly update the nodes rather than get + update, and ignore version errors.  Since
		// `rootRotationReconciler` should be hooked up to all node update/delete/create vents, we should have
		// close to the latest versions of all the nodes.  If not, the node will updated later and the
		// next batch of updates should catch it.
		for _, n := range toUpdate {
			if err := batch.Update(func(tx store.Tx) error {
				return store.UpdateNode(tx, n)
			}); err != nil {
				log.G(r.ctx).WithError(err).Debugf("unable to update node %s to request a certificate rotation")
			}
		}
		return nil
	})
	return err
}
