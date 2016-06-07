package test

import (
	"net"
	"testing"
	"time"

	"github.com/docker/swarmkit/manager/raftpicker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

type testRouteGuide struct{}

func (testRouteGuide) GetFeature(context.Context, *Point) (*Feature, error) {
	panic("not implemented")
}

func (testRouteGuide) ListFeatures(*Rectangle, RouteGuide_ListFeaturesServer) error {
	panic("not implemented")
}

func (testRouteGuide) RecordRoute(RouteGuide_RecordRouteServer) error {
	panic("not implemented")
}

func (testRouteGuide) RouteChat(RouteGuide_RouteChatServer) error {
	panic("not implemented")
}

type mockCluster struct {
	addr string
}

func (m *mockCluster) LeaderAddr() (string, error) {
	return m.addr, nil
}

func (m *mockCluster) IsLeader() bool {
	return false
}

func TestSimpleRedirect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)
	defer conn.Close()

	cluster := &mockCluster{addr: addr}
	cs := raftpicker.NewConnSelector(cluster, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))

	forwardAsOwnRequest := func(ctx context.Context) (context.Context, error) { return ctx, nil }
	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, cs, cluster, forwardAsOwnRequest)
	srv := grpc.NewServer()
	RegisterRouteGuideServer(srv, api)
	go srv.Serve(l)
	defer srv.Stop()

	client := NewRouteGuideClient(conn)
	_, err = client.GetFeature(context.Background(), &Point{})
	assert.NotNil(t, err)
	assert.Equal(t, codes.ResourceExhausted, grpc.Code(err))
}

func TestServerStreamRedirect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)
	defer conn.Close()

	cluster := &mockCluster{addr: addr}
	cs := raftpicker.NewConnSelector(cluster, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))

	forwardAsOwnRequest := func(ctx context.Context) (context.Context, error) { return ctx, nil }
	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, cs, cluster, forwardAsOwnRequest)
	srv := grpc.NewServer()
	RegisterRouteGuideServer(srv, api)
	go srv.Serve(l)
	defer srv.Stop()

	client := NewRouteGuideClient(conn)
	stream, err := client.ListFeatures(context.Background(), &Rectangle{})
	// err not nil is only on network issues
	assert.Nil(t, err)
	_, err = stream.Recv()
	assert.NotNil(t, err)
	assert.Equal(t, codes.ResourceExhausted, grpc.Code(err))
}

func TestClientStreamRedirect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)
	defer conn.Close()

	cluster := &mockCluster{addr: addr}
	cs := raftpicker.NewConnSelector(cluster, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))

	forwardAsOwnRequest := func(ctx context.Context) (context.Context, error) { return ctx, nil }
	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, cs, cluster, forwardAsOwnRequest)
	srv := grpc.NewServer()
	RegisterRouteGuideServer(srv, api)
	go srv.Serve(l)
	defer srv.Stop()

	client := NewRouteGuideClient(conn)
	stream, err := client.RecordRoute(context.Background())
	// err not nil is only on network issues
	assert.Nil(t, err)
	// any Send will be ok, I don't know why
	assert.Nil(t, stream.Send(&Point{}))
	_, err = stream.CloseAndRecv()
	assert.NotNil(t, err)
	assert.Equal(t, codes.ResourceExhausted, grpc.Code(err))
}

func TestClientServerStreamRedirect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	addr := l.Addr().String()
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)
	defer conn.Close()

	cluster := &mockCluster{addr: addr}
	cs := raftpicker.NewConnSelector(cluster, grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))

	forwardAsOwnRequest := func(ctx context.Context) (context.Context, error) { return ctx, nil }
	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, cs, cluster, forwardAsOwnRequest)
	srv := grpc.NewServer()
	RegisterRouteGuideServer(srv, api)
	go srv.Serve(l)
	defer srv.Stop()

	client := NewRouteGuideClient(conn)
	stream, err := client.RouteChat(context.Background())
	// err not nil is only on network issues
	assert.Nil(t, err)
	// any Send will be ok, I don't know why
	assert.Nil(t, stream.Send(&RouteNote{}))
	_, err = stream.Recv()
	assert.NotNil(t, err)
	assert.Equal(t, codes.ResourceExhausted, grpc.Code(err))
}
