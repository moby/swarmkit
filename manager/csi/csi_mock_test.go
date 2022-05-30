package csi

import (
	"context"
	"fmt"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/akutz/memconn"
	"github.com/rexray/gocsi/mock/provider"
	"google.golang.org/grpc"
)

// startMockServer is strongly based on the function of the equivalent name in
// the github.com/rexray/gocsi/testing package. It creates a mock CSI storage
// provider which can be used for testing. name indicates the name of the mock,
// which allows for testing situation with multiple CSI drivers installed.
//
// returns a client connection to a new mock CSI provider, and a cancel
// function to call when cleaing up.
func startMockServer(ctx context.Context, name string) (*grpc.ClientConn, func()) {
	sp := provider.New()
	// the memconn package lets us avoid the use of a unix domain socket, which
	// helps make the tests more portable.
	lis, err := memconn.Listen("memu", fmt.Sprintf("csi-test-%v", name))
	Expect(err).To(BeNil())

	go func() {
		defer GinkgoRecover()
		if err := sp.Serve(ctx, lis); err != nil {
			Expect(err).To(Equal("http: Server closed"))
		}
	}()

	clientOpts := []grpc.DialOption{
		grpc.WithInsecure(),
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			return memconn.Dial("memu", fmt.Sprintf("csi-test-%v", name))
		}),
	}

	client, err := grpc.DialContext(ctx, "", clientOpts...)
	Expect(err).ToNot(HaveOccurred())

	return client, func() {
		lis.Close()
		sp.GracefulStop(ctx)
	}
}
