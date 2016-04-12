package test

import (
	"net"
	"testing"
	"time"

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

type testLeaderConn struct{}

func (testLeaderConn) LeaderConn() (*grpc.ClientConn, error) {
	panic("not implemented")
}

func TestNew(t *testing.T) {
	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, testLeaderConn{})
	proxy, ok := api.(*raftProxyRouteGuideServer)
	assert.True(t, ok, "wrong proxy type")
	assert.NotNil(t, proxy.local)
	assert.NotNil(t, proxy.leaders)
}

type mockConn struct {
	conn *grpc.ClientConn
}

func (m *mockConn) LeaderConn() (*grpc.ClientConn, error) {
	return m.conn, nil
}

func TestSimpleRedirect(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)
	defer conn.Close()

	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, &mockConn{conn: conn})
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
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)

	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, &mockConn{conn: conn})
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
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)

	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, &mockConn{conn: conn})
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
	conn, err := grpc.Dial(l.Addr().String(), grpc.WithInsecure(), grpc.WithTimeout(5*time.Second))
	require.NoError(t, err)

	api := NewRaftProxyRouteGuideServer(testRouteGuide{}, &mockConn{conn: conn})
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
