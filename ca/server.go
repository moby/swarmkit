package ca

import (
	"crypto/subtle"
	"fmt"
	"sync"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/identity"
	"github.com/docker/swarm-v2/log"
	"github.com/docker/swarm-v2/manager/state"
	"github.com/docker/swarm-v2/manager/state/store"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Server is the CA and NodeCA API gRPC server.
// TODO(aaronl): At some point we may want to have separate implementations of
// CA, NodeCA, and other hypothetical future CA services. At the moment,
// breaking it apart doesn't seem worth it.
type Server struct {
	mu               sync.Mutex
	wg               sync.WaitGroup
	ctx              context.Context
	cancel           func()
	store            *store.MemoryStore
	securityConfig   *SecurityConfig
	acceptancePolicy *api.AcceptancePolicy
}

// DefaultAcceptancePolicy returns the default acceptance policy.
func DefaultAcceptancePolicy() api.AcceptancePolicy {
	return api.AcceptancePolicy{
		Autoaccept: map[string]bool{AgentRole: true},
	}
}

// NewServer creates a CA API server.
func NewServer(store *store.MemoryStore, securityConfig *SecurityConfig) *Server {
	return &Server{
		store:          store,
		securityConfig: securityConfig,
	}
}

// NodeCertificateStatus returns the current issuance status of an issuance request identified by the nodeID
func (s *Server) NodeCertificateStatus(ctx context.Context, request *api.NodeCertificateStatusRequest) (*api.NodeCertificateStatusResponse, error) {
	if request.NodeID == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	if err := s.addTask(); err != nil {
		return nil, err
	}
	defer s.doneTask()

	var node *api.Node

	event := state.EventUpdateNode{
		Node:   &api.Node{ID: request.NodeID},
		Checks: []state.NodeCheckFunc{state.NodeCheckID},
	}

	// Retrieve the current value of the certificate with this token, and create a watcher
	updates, cancel, err := store.ViewAndWatch(
		s.store,
		func(tx store.ReadTx) error {
			node = store.GetNode(tx, request.NodeID)
			return nil
		},
		event,
	)
	if err != nil {
		return nil, err
	}
	defer cancel()

	// This node ID doesn't exist
	if node == nil {
		return nil, grpc.Errorf(codes.NotFound, codes.NotFound.String())
	}

	log.G(ctx).WithFields(logrus.Fields{
		"node.id": node.ID,
		"status":  node.Certificate.Status,
		"method":  "NodeCertificateStatus",
	})

	// If this certificate has a final state, return it immediately (both pending and renew are transition states)
	if isFinalState(node.Certificate.Status) {
		return &api.NodeCertificateStatusResponse{
			Status:      &node.Certificate.Status,
			Certificate: &node.Certificate,
		}, nil
	}

	log.G(ctx).WithFields(logrus.Fields{
		"node.id": node.ID,
		"status":  node.Certificate.Status,
		"method":  "NodeCertificateStatus",
	}).Debugf("started watching for certificate updates")

	// Certificate is Pending or in an Unknown state, let's wait for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.(type) {
			case state.EventUpdateNode:
				// We got an update on the certificate record. If the status is a final state,
				// return the certificate.
				if isFinalState(v.Node.Certificate.Status) {
					cert := v.Node.Certificate.Copy()
					return &api.NodeCertificateStatusResponse{
						Status:      &cert.Status,
						Certificate: cert,
					}, nil
				}
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.ctx.Done():
			return nil, s.ctx.Err()
		}
	}
}

// IssueNodeCertificate receives requests from a remote client indicating a node type and a CSR,
// returning a certificate chain signed by the local CA, if available.
func (s *Server) IssueNodeCertificate(ctx context.Context, request *api.IssueNodeCertificateRequest) (*api.IssueNodeCertificateResponse, error) {
	if request.CSR == nil || request.Role == "" {
		return nil, grpc.Errorf(codes.InvalidArgument, codes.InvalidArgument.String())
	}

	if err := s.addTask(); err != nil {
		return nil, err
	}
	defer s.doneTask()

	// If the remote node is an Agent (either forwarded by a manager, or calling directly),
	// issue a renew agent certificate entry with the correct ID
	nodeID, err := AuthorizeForwardedRoleAndOrg(ctx, []string{AgentRole}, []string{ManagerRole}, s.securityConfig.ClientTLSCreds.Organization())
	if err == nil {
		return s.issueRenewCertificate(ctx, nodeID, AgentRole, request.CSR)
	}

	// If the remote node is a Manager, issue a renew certificate entry with the correct ID
	nodeID, err = AuthorizeForwardedRoleAndOrg(ctx, []string{ManagerRole}, []string{ManagerRole}, s.securityConfig.ClientTLSCreds.Organization())
	if err == nil {
		return s.issueRenewCertificate(ctx, nodeID, ManagerRole, request.CSR)
	}

	// The remote node didn't successfully present a valid MTLS certificate, let's issue
	// a pending certificate with a new ID

	// If there is a secret configured, the client has to have supplied it to be able to propose itself
	// for the first certificate issuance

	s.mu.Lock()
	if s.acceptancePolicy.Secret != "" {
		if request.Secret == "" ||
			subtle.ConstantTimeCompare([]byte(request.Secret), []byte(s.acceptancePolicy.Secret)) != 1 {
			s.mu.Unlock()
			return nil, fmt.Errorf("A valid secret token is necessary to join this cluster")
		}
	}
	s.mu.Unlock()

	// Max number of collisions of ID or CN to tolerate before giving up
	maxRetries := 3

	// Generate a random ID for this new node
	for i := 0; ; i++ {
		nodeID = identity.NewNodeID()

		err := s.store.Update(func(tx store.Tx) error {
			node := &api.Node{
				ID: nodeID,
				Certificate: api.Certificate{
					CSR:  request.CSR,
					CN:   nodeID,
					Role: request.Role,
					Status: api.IssuanceStatus{
						State: api.IssuanceStatePending,
					},
				},
			}
			return store.CreateNode(tx, node)
		})
		if err == nil {
			log.G(ctx).WithFields(logrus.Fields{
				"node.id":   nodeID,
				"node.role": request.Role,
				"method":    "IssueNodeCertificate",
			}).Debugf("new certificate entry added")
			break
		}
		if err != store.ErrExist {
			return nil, err
		}
		if i == maxRetries {
			return nil, err
		}
		log.G(ctx).WithFields(logrus.Fields{
			"node.id":   nodeID,
			"node.role": request.Role,
			"method":    "IssueNodeCertificate",
		}).Errorf("randomly generated node ID collided with an existing one - retrying")
	}

	return &api.IssueNodeCertificateResponse{
		NodeID: nodeID,
	}, nil
}

func (s *Server) issueRenewCertificate(ctx context.Context, nodeID, role string, csr []byte) (*api.IssueNodeCertificateResponse, error) {
	err := s.store.Update(func(tx store.Tx) error {
		cert := api.Certificate{
			CSR:  csr,
			CN:   nodeID,
			Role: role,
			Status: api.IssuanceStatus{
				State: api.IssuanceStateRenew,
			},
		}

		node := store.GetNode(tx, nodeID)
		if node == nil {
			return store.CreateNode(tx, &api.Node{
				ID:          nodeID,
				Certificate: cert,
			})
		}

		node.Certificate = cert
		return store.UpdateNode(tx, node)
	})
	if err != nil {
		return nil, err
	}

	log.G(ctx).WithFields(logrus.Fields{
		"node.id":   nodeID,
		"node.role": role,
		"method":    "issueRenewCertificate",
	}).Debugf("node certificate updated")
	return &api.IssueNodeCertificateResponse{
		NodeID: nodeID,
	}, nil
}

// GetRootCACertificate returns the certificate of the Root CA.
func (s *Server) GetRootCACertificate(ctx context.Context, request *api.GetRootCACertificateRequest) (*api.GetRootCACertificateResponse, error) {
	log.G(ctx).WithFields(logrus.Fields{
		"method": "GetRootCACertificate",
	})

	return &api.GetRootCACertificateResponse{
		Certificate: s.securityConfig.RootCA().Cert,
	}, nil
}

// Run runs the CA signer main loop.
// The CA signer can be stopped with cancelling ctx or calling Stop().
func (s *Server) Run(ctx context.Context) error {
	s.mu.Lock()
	if s.isRunning() {
		s.mu.Unlock()
		return fmt.Errorf("CA signer is stopped")
	}
	s.wg.Add(1)
	defer s.wg.Done()
	logger := log.G(ctx).WithField("module", "ca")
	ctx = log.WithLogger(ctx, logger)
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.mu.Unlock()

	var nodes []*api.Node
	updates, cancel, err := store.ViewAndWatch(
		s.store,
		func(readTx store.ReadTx) error {
			clusters, err := store.FindClusters(readTx, store.ByName(store.DefaultClusterName))
			if err != nil {
				return err
			}
			if len(clusters) != 1 {
				return fmt.Errorf("could not find cluster object")
			}
			s.updateCluster(ctx, clusters[0])

			nodes, err = store.FindNodes(readTx, store.All)
			return err
		},
		state.EventCreateNode{},
		state.EventUpdateNode{},
		state.EventUpdateCluster{},
	)
	if err != nil {
		log.G(ctx).WithFields(logrus.Fields{
			"method": "(*Server).Run",
		}).WithError(err).Errorf("snapshot store view failed")
		return err
	}
	defer cancel()

	if err := s.reconcileNodeCertificates(ctx, nodes); err != nil {
		// We don't return here because that means the Run loop would
		// never run. Log an error instead.
		log.G(ctx).WithFields(logrus.Fields{
			"method": "(*Server).Run",
		}).WithError(err).Errorf("error attempting to reconcile certificates")
	}

	// Watch for changes.
	for {
		select {
		case event := <-updates:
			switch v := event.(type) {
			case state.EventCreateNode:
				s.evaluateAndSignNodeCert(ctx, v.Node)
			case state.EventUpdateNode:
				s.evaluateAndSignNodeCert(ctx, v.Node)
			case state.EventUpdateCluster:
				s.updateCluster(ctx, v.Cluster)
			}

		case <-ctx.Done():
			return ctx.Err()
		case <-s.ctx.Done():
			return nil
		}
	}
}

// Stop stops the CA and closes all grpc streams.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.isRunning() {
		return fmt.Errorf("CA signer is already stopped")
	}
	s.cancel()
	s.mu.Unlock()
	// wait for all handlers to finish their CA deals,
	s.wg.Wait()
	return nil
}

func (s *Server) addTask() error {
	s.mu.Lock()
	if !s.isRunning() {
		s.mu.Unlock()
		return grpc.Errorf(codes.Aborted, "CA signer is stopped")
	}
	s.wg.Add(1)
	s.mu.Unlock()
	return nil
}

func (s *Server) doneTask() {
	s.wg.Done()
}

func (s *Server) isRunning() bool {
	if s.ctx == nil {
		return false
	}
	select {
	case <-s.ctx.Done():
		return false
	default:
	}
	return true
}

func (s *Server) updateCluster(ctx context.Context, cluster *api.Cluster) {
	s.mu.Lock()
	s.acceptancePolicy = cluster.Spec.AcceptancePolicy.Copy()
	s.mu.Unlock()
	if cluster.RootCA != nil && len(cluster.RootCA.CACert) != 0 && len(cluster.RootCA.CAKey) != 0 {
		log.G(ctx).Debug("updating root CA object from raft")
		err := s.securityConfig.UpdateRootCA(cluster.RootCA.CACert, cluster.RootCA.CAKey)
		if err != nil {
			log.G(ctx).WithError(err).Error("updating root key failed")
		}
	}
}

func (s *Server) setNodeCertState(node *api.Node, state api.IssuanceState) error {
	return s.store.Update(func(tx store.Tx) error {
		latestNode := store.GetNode(tx, node.ID)
		if latestNode == nil {
			return store.ErrNotExist
		}

		// Remote users are expecting a full certificate chain, not just a signed certificate
		latestNode.Certificate.Status = api.IssuanceStatus{
			State: state,
		}

		return store.UpdateNode(tx, latestNode)
	})
}

func (s *Server) evaluateAndSignNodeCert(ctx context.Context, node *api.Node) {
	// If the desired membership and actual state are in sync, there's
	// nothing to do.
	if node.Spec.Membership == api.NodeMembershipAccepted && node.Certificate.Status.State == api.IssuanceStateIssued {
		return
	}
	if node.Spec.Membership == api.NodeMembershipRejected && node.Certificate.Status.State == api.IssuanceStateRejected {
		return
	}

	// If the desired membership was set to rejected, we should
	// act on that right away, and that is all that should be done.
	if node.Spec.Membership == api.NodeMembershipRejected {
		err := s.setNodeCertState(node, api.IssuanceStateRejected)
		if err != nil {
			log.G(ctx).WithFields(logrus.Fields{
				"node.id":   node.ID,
				"node.role": node.Certificate.Role,
				"method":    "(*Server).evaluateAndSignCert",
			}).WithError(err).Errorf("failed to change certificate state")
		}
		return
	}

	// If the certificate state is renew, then it is a server-sided accepted cert (cert renewals)
	if node.Certificate.Status.State == api.IssuanceStateRenew {
		s.signNodeCert(ctx, node)
		return
	}

	// If the certificate state is not pending at this point, we are in an unknown state, return
	if node.Certificate.Status.State != api.IssuanceStatePending {
		return
	}

	// Check to see if our autoacceptance policy allows this node to be issued without manual intervention
	s.mu.Lock()
	if s.acceptancePolicy.Autoaccept != nil && s.acceptancePolicy.Autoaccept[node.Certificate.Role] {
		s.mu.Unlock()
		s.signNodeCert(ctx, node)
		return
	}
	s.mu.Unlock()

	// Only issue this node if the admin explicitly changed it to Accepted
	if node.Spec.Membership == api.NodeMembershipAccepted {
		// Cert was approved by admin
		s.signNodeCert(ctx, node)
	}
}

func (s *Server) signNodeCert(ctx context.Context, node *api.Node) {
	if !s.securityConfig.RootCA().CanSign() {
		log.G(ctx).Error("no valid signer found")
		return
	}

	node = node.Copy()
	nodeID := node.ID

	for {
		cert, err := s.securityConfig.RootCA().ParseValidateAndSignCSR(node.Certificate.CSR, node.Certificate.CN, node.Certificate.Role, s.securityConfig.ClientTLSCreds.Organization())
		if err != nil {
			log.G(ctx).WithFields(logrus.Fields{
				"node.id": node.ID,
				"method":  "(*Server).signNodeCert",
			}).WithError(err).Errorf("failed to parse CSR")
		}

		err = s.store.Update(func(tx store.Tx) error {
			// Remote users are expecting a full certificate chain, not just a signed certificate
			node.Certificate.Certificate = append(cert, s.securityConfig.RootCA().Cert...)
			node.Certificate.Status = api.IssuanceStatus{
				State: api.IssuanceStateIssued,
			}

			err := store.UpdateNode(tx, node)
			if err != nil {
				node = store.GetNode(tx, nodeID)
				if node == nil {
					err = fmt.Errorf("node %s does not exist", nodeID)
				}
			}
			return err
		})
		if err == nil {
			log.G(ctx).WithFields(logrus.Fields{
				"node.id":   node.ID,
				"node.role": node.Certificate.Role,
				"method":    "(*Server).signNodeCert",
			}).Debugf("certificate issued")
			break
		}
		if err == store.ErrSequenceConflict {
			continue
		}

		log.G(ctx).WithFields(logrus.Fields{
			"node.id": node.ID,
			"method":  "(*Server).signNodeCert",
		}).WithError(err).Errorf("transaction failed")
		return
	}

}

func (s *Server) reconcileNodeCertificates(ctx context.Context, nodes []*api.Node) error {
	for _, node := range nodes {
		s.evaluateAndSignNodeCert(ctx, node)
	}

	return nil
}

func isFinalState(status api.IssuanceStatus) bool {
	if status.State != api.IssuanceStatePending &&
		status.State != api.IssuanceStateRenew {
		return true
	}

	return false
}
