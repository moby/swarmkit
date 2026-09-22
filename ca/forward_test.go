package ca_test

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/manager/raftselector"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

const org = "swarm-test-org"

// The keys WithMetadataForwardTLSInfo writes, spelled out to pin the wire
// format: certForwardedKey and friends in forward.go.
const (
	forwardedKey  = "forwarded_cert"
	cnKey         = "forwarded_cert_cn"
	orgKey        = "forwarded_cert_org"
	ouKey         = "forwarded_cert_ou"
	remoteAddrKey = "remote_addr"
)

// recorder is a Health service which records the context of the last request
// it served.
type recorder struct {
	mu  sync.Mutex
	ctx context.Context
}

func (r *recorder) Check(ctx context.Context, _ *api.HealthCheckRequest) (*api.HealthCheckResponse, error) {
	r.mu.Lock()
	r.ctx = ctx
	r.mu.Unlock()
	return &api.HealthCheckResponse{Status: api.HealthCheckResponse_SERVING}, nil
}

// take returns the context of the last request served and forgets it, or
// returns nil if no request was served since the last call. The RPC has
// finished by the time a test reads the context, so it is cancelled; every
// value on it stays readable, which is all the ca authorization helpers look
// at.
func (r *recorder) take() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	ctx := r.ctx
	r.ctx = nil
	return ctx
}

type nodeCreds struct {
	id     string
	role   string
	server credentials.TransportCredentials
	client credentials.TransportCredentials
}

func newNodeCreds(t *testing.T, rootCA *ca.RootCA, cn, ou string) nodeCreds {
	t.Helper()
	paths := ca.NewConfigPaths(t.TempDir())
	cert, _, err := rootCA.IssueAndSaveNewCertificates(ca.NewKeyReadWriter(paths.Node, nil, nil), cn, ou, org)
	require.NoError(t, err)

	serverCreds, err := rootCA.NewServerTLSCredentials(cert)
	require.NoError(t, err)
	clientCreds, err := rootCA.NewClientTLSCredentials(cert, ca.ManagerRole)
	require.NoError(t, err)

	return nodeCreds{id: cn, role: ou, server: serverCreds, client: clientCreds}
}

func serveHop(t *testing.T, creds credentials.TransportCredentials, srv api.HealthServer) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	grpcServer := grpc.NewServer(grpc.Creds(creds))
	api.RegisterHealthServer(grpcServer, srv)
	go grpcServer.Serve(l)
	t.Cleanup(grpcServer.Stop)

	return l.Addr().String()
}

func dialConn(t *testing.T, addr string, creds credentials.TransportCredentials) *grpc.ClientConn {
	t.Helper()
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(creds))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })
	return conn
}

// leaderConn is a raftselector.ConnProvider which always names the same node as
// the leader, or reports that this node is itself the leader when conn is nil.
type leaderConn struct {
	conn *grpc.ClientConn
}

func (l leaderConn) LeaderConn(context.Context) (*grpc.ClientConn, error) {
	if l.conn == nil {
		return nil, raftselector.ErrIsLeader
	}
	return l.conn, nil
}

// serveProxy serves a raft proxy for the Health service on node, with
// WithMetadataForwardTLSInfo as its remote context modifier the way manager.go
// wires the proxies on the remote API. It forwards every request to leader, or
// serves it with local when leader is nil.
func serveProxy(t *testing.T, node nodeCreds, local api.HealthServer, leader *grpc.ClientConn) string {
	t.Helper()
	return serveHop(t, node.server, api.NewRaftProxyHealthServer(local, leaderConn{leader}, nil, ca.WithMetadataForwardTLSInfo))
}

// swarm is a worker and two managers whose certificates share a root CA. Both
// managers serve raft proxies: the leader serves every request itself and
// records it in served, and the follower forwards every request to the leader.
type swarm struct {
	rootCA   ca.RootCA
	worker   nodeCreds
	follower nodeCreds
	leader   nodeCreds
	served   *recorder

	leaderAddr   string
	followerAddr string
}

func newSwarm(t *testing.T) *swarm {
	t.Helper()
	rootCA, err := ca.CreateRootCA("rootCN")
	require.NoError(t, err)

	s := &swarm{rootCA: rootCA, served: &recorder{}}
	s.worker = newNodeCreds(t, &s.rootCA, "worker-node", ca.WorkerRole)
	s.follower = newNodeCreds(t, &s.rootCA, "manager-1", ca.ManagerRole)
	s.leader = newNodeCreds(t, &s.rootCA, "manager-2", ca.ManagerRole)

	s.leaderAddr = serveProxy(t, s.leader, s.served, nil)
	s.followerAddr = serveProxy(t, s.follower, nil, dialConn(t, s.leaderAddr, s.follower.client))
	return s
}

// send makes a Health check against addr as the holder of creds, with md as the
// request's metadata. It returns the context the leader served the request
// with, or nil if the request never reached the leader, and the error the call
// ended with.
func (s *swarm) send(t *testing.T, addr string, creds credentials.TransportCredentials, md metadata.MD) (context.Context, error) {
	t.Helper()
	ctx := t.Context()
	if md != nil {
		ctx = metadata.NewOutgoingContext(ctx, md)
	}
	_, err := api.NewHealthClient(dialConn(t, addr, creds)).Check(ctx, &api.HealthCheckRequest{Service: "Health"})
	return s.served.take(), err
}

// forwardedMD is the forwarded TLS information a manager sends to say that it
// is forwarding a request on node's behalf, with marker as the value of the
// key which marks the request as forwarded.
func forwardedMD(node nodeCreds, role string, marker ...string) metadata.MD {
	return metadata.MD{
		forwardedKey: marker,
		cnKey:        {node.id},
		orgKey:       {org},
		ouKey:        {role},
	}
}

// TestWithMetadataForwardTLSInfo asserts that a follower forwards a request to
// the leader as the node which sent it, vouched for by the follower, and that
// it refuses to forward a request which arrived already forwarded. Forwarding
// it again would present the node which forwarded it, rather than the node
// which sent it, as the caller.
//
// The raft proxy's redirect guard refuses an already-forwarded request first
// when the node which forwarded it passed the guard's "redirect" key along.
// Every row here leaves the key out, as managers running older versions of
// swarmkit do, so it is WithMetadataForwardTLSInfo which has to refuse it.
func TestWithMetadataForwardTLSInfo(t *testing.T) {
	s := newSwarm(t)
	other := newNodeCreds(t, &s.rootCA, "manager-3", ca.ManagerRole)

	for _, tt := range []struct {
		name    string
		client  nodeCreds
		md      metadata.MD
		refused bool
	}{
		{
			name:   "a worker speaking for itself",
			client: s.worker,
		},
		{
			name:    "forwarded by another manager",
			client:  other,
			md:      forwardedMD(s.worker, ca.WorkerRole, "true"),
			refused: true,
		},
		{
			name:    "forwarding forged by a worker",
			client:  s.worker,
			md:      forwardedMD(other, ca.ManagerRole, "true"),
			refused: true,
		},
		{
			// The marker is read exactly as the leader reads it, so anything
			// the leader would not take for a forwarded request is forwarded
			// once, as the node which sent it.
			name:   "forwarded marker is not true",
			client: s.worker,
			md:     forwardedMD(other, ca.ManagerRole, "false"),
		},
		{
			name:   "forwarded marker is repeated",
			client: s.worker,
			md:     forwardedMD(other, ca.ManagerRole, "true", "true"),
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			leaderCtx, err := s.send(t, s.followerAddr, tt.client.client, tt.md)

			if tt.refused {
				assert.Equal(t, codes.ResourceExhausted, status.Code(err))
				assert.Contains(t, status.Convert(err).Message(), "already forwarded")
				assert.Nil(t, leaderCtx, "the request was forwarded a second time")
				return
			}

			require.NoError(t, err)
			require.NotNil(t, leaderCtx, "the leader never served the request")

			caller, err := ca.RemoteNode(leaderCtx)
			require.NoError(t, err)
			assert.Equal(t, tt.client.id, caller.NodeID)
			assert.Equal(t, []string{tt.client.role}, caller.Roles)
			assert.Equal(t, org, caller.Organization)
			assert.NotEmpty(t, caller.RemoteAddr)

			if assert.NotNil(t, caller.ForwardedBy) {
				assert.Equal(t, s.follower.id, caller.ForwardedBy.NodeID)
				assert.Equal(t, []string{s.follower.role}, caller.ForwardedBy.Roles)
				assert.Equal(t, org, caller.ForwardedBy.Organization)
				assert.NotEqual(t, caller.RemoteAddr, caller.ForwardedBy.RemoteAddr,
					"the client's address was replaced with the follower's")
			}

			// Every row which is served is sent by the worker.
			t.Run("authorization uses the original identity", func(t *testing.T) {
				id, err := ca.AuthorizeForwardedRoleAndOrg(leaderCtx, []string{ca.WorkerRole}, []string{ca.ManagerRole}, org, nil)
				assert.NoError(t, err)
				assert.Equal(t, tt.client.id, id)

				_, err = ca.AuthorizeForwardedRoleAndOrg(leaderCtx, []string{ca.ManagerRole}, []string{ca.ManagerRole}, org, nil)
				assert.Equal(t, codes.PermissionDenied, status.Code(err),
					"a forwarded worker request cleared a manager-only gate")
			})
		})
	}
}

// TestWithMetadataForwardTLSInfoOutgoingMetadata asserts that
// WithMetadataForwardTLSInfo leaves the outgoing metadata it was given alone,
// save for the TLS info keys, which it always fills in from its own peer. With
// no peer to describe it says so with blank TLS info, leaving no address
// behind.
func TestWithMetadataForwardTLSInfoOutgoingMetadata(t *testing.T) {
	s := newSwarm(t)

	// The leader serves a request sent straight to it, which leaves a handler
	// context carrying the worker's real peer and TLS information.
	ctx, err := s.send(t, s.leaderAddr, s.worker.client, nil)
	require.NoError(t, err)
	require.NotNil(t, ctx, "the leader never served the request")

	p, ok := peer.FromContext(ctx)
	require.True(t, ok, "handler context had no peer")

	own := metadata.MD{
		forwardedKey:  {"true"},
		cnKey:         {s.worker.id},
		orgKey:        {org},
		ouKey:         {ca.WorkerRole},
		remoteAddrKey: {p.Addr.String()},
	}
	// Metadata which has nothing to do with forwarded TLS info, such as the
	// raft proxy's redirect loop guard.
	unrelated := metadata.MD{
		"redirect": {"10.0.0.1:2377", "10.0.0.2:2377"},
		"x-custom": {"keep", "me"},
	}
	forwarded := metadata.MD{
		forwardedKey:  {"true"},
		cnKey:         {"original-node"},
		orgKey:        {"original-org"},
		ouKey:         {ca.ManagerRole},
		remoteAddrKey: {"10.0.0.3:2377"},
	}
	stale := forwarded.Copy()
	delete(stale, forwardedKey)
	blank := metadata.MD{
		forwardedKey: {"true"},
		cnKey:        {""},
		orgKey:       {""},
		ouKey:        {},
	}

	for _, tt := range []struct {
		name     string
		noPeer   bool
		outgoing metadata.MD
		want     metadata.MD
	}{
		{
			name: "no outgoing metadata",
			want: own,
		},
		{
			name:     "unrelated metadata is kept",
			outgoing: unrelated.Copy(),
			want:     metadata.Join(unrelated, own),
		},
		{
			name:     "unmarked TLS info is replaced",
			outgoing: metadata.Join(unrelated, stale),
			want:     metadata.Join(unrelated, own),
		},
		{
			name:     "marked TLS info is replaced",
			outgoing: metadata.Join(unrelated, forwarded),
			want:     metadata.Join(unrelated, own),
		},
		{
			name:   "no peer states a blank identity",
			noPeer: true,
			want:   blank,
		},
		{
			// Blank TLS info next to somebody else's address would describe a
			// caller which never existed.
			name:     "no peer drops a stale remote address",
			noPeer:   true,
			outgoing: forwarded.Copy(),
			want:     blank,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			callCtx := ctx
			if tt.noPeer {
				callCtx = context.Background()
			}
			if tt.outgoing != nil {
				callCtx = metadata.NewOutgoingContext(callCtx, tt.outgoing)
			}
			before := tt.outgoing.Copy()

			fwdCtx, err := ca.WithMetadataForwardTLSInfo(callCtx)
			require.NoError(t, err)

			got, _ := metadata.FromOutgoingContext(fwdCtx)
			assert.Equal(t, tt.want, got)
			assert.Equal(t, before, tt.outgoing.Copy(), "the caller's metadata was modified in place")
		})
	}
}

// TestRaftProxyRedirectGuard asserts that the raft proxy still refuses to
// forward a request which has already been forwarded once, with
// WithMetadataForwardTLSInfo wired up as its remote context modifier the way
// manager.go wires it. The guard travels as a "redirect" key in the outgoing
// metadata, so it only survives to the next hop if WithMetadataForwardTLSInfo
// leaves metadata it did not write alone.
//
// WithMetadataForwardTLSInfo refuses an already-forwarded request itself, which
// would hide a lost "redirect" key. The second hop forwards with a modifier
// which leaves the context alone, so only the guard can refuse the request
// there.
func TestRaftProxyRedirectGuard(t *testing.T) {
	s := newSwarm(t)

	// The follower forwards straight to the leader. second forwards to the
	// leader too, and first forwards to second, so reaching the leader from
	// first takes two redirects.
	second := newNodeCreds(t, &s.rootCA, "manager-3", ca.ManagerRole)
	first := newNodeCreds(t, &s.rootCA, "manager-4", ca.ManagerRole)
	forwardUnchanged := func(ctx context.Context) (context.Context, error) { return ctx, nil }
	secondAddr := serveHop(t, second.server, api.NewRaftProxyHealthServer(nil, leaderConn{dialConn(t, s.leaderAddr, second.client)}, nil, forwardUnchanged))
	firstAddr := serveProxy(t, first, nil, dialConn(t, secondAddr, first.client))

	t.Run("one redirect is served", func(t *testing.T) {
		leaderCtx, err := s.send(t, s.followerAddr, s.worker.client, nil)
		require.NoError(t, err)
		assert.NotNil(t, leaderCtx, "the leader never served the request")
	})

	t.Run("two redirects are refused", func(t *testing.T) {
		leaderCtx, err := s.send(t, firstAddr, s.worker.client, nil)
		require.Error(t, err)
		assert.Equal(t, codes.ResourceExhausted, status.Code(err))
		assert.Nil(t, leaderCtx, "the request reached the leader")
	})
}

// TestForwardWithoutClientCert asserts that a request from a client which
// presented no certificate is forwarded with blank TLS info rather than with
// none at all, so the leader denies it instead of taking it for a request the
// follower made on its own behalf.
func TestForwardWithoutClientCert(t *testing.T) {
	s := newSwarm(t)

	// A client which verifies the server but presents no certificate of its
	// own. The servers let it in because NewServerTLSConfig only verifies a
	// client certificate when one is given.
	anonTLS, err := ca.NewClientTLSConfig(nil, s.rootCA.Pool, ca.ManagerRole)
	require.NoError(t, err)
	anon := credentials.NewTLS(anonTLS)

	leaderCtx, err := s.send(t, s.followerAddr, anon, nil)
	require.NoError(t, err)
	require.NotNil(t, leaderCtx, "the leader never served the request")

	// The request is marked as forwarded, so the leader cannot read it as one
	// the follower made itself...
	md, ok := metadata.FromIncomingContext(leaderCtx)
	require.True(t, ok)
	assert.Equal(t, []string{"true"}, md.Get(forwardedKey))

	// ...but it names nobody, so there is no identity to act on.
	_, err = ca.RemoteNode(leaderCtx)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Contains(t, status.Convert(err).Message(), "missing information in forwarded request")

	for _, roles := range [][]string{{ca.WorkerRole}, {ca.ManagerRole}} {
		_, err := ca.AuthorizeForwardedRoleAndOrg(leaderCtx, roles, []string{ca.ManagerRole}, org, nil)
		assert.Equal(t, codes.PermissionDenied, status.Code(err),
			"an anonymous forwarded request was authorized as %v", roles)
	}
}
