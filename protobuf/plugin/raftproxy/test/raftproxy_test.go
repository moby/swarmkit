package test

import (
	"testing"

	"golang.org/x/net/context"

	"google.golang.org/grpc"
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
	if !ok {
		t.Fatalf("unexpected type: %T", api)
	}
	if proxy.local == nil {
		t.Fatalf("local instance must be set")
	}
	if proxy.leaders == nil {
		t.Fatalf("leaders must be set")
	}
}
