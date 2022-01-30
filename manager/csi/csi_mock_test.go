package csi

// FIXME: github.com/rexray/gocsi outdated
// github.com/docker/swarmkit/manager/csi tested by
// 		github.com/docker/swarmkit/manager/csi.test imports
// 		github.com/rexray/gocsi/mock/provider imports
// 		github.com/rexray/gocsi imports
// 		github.com/rexray/gocsi/middleware/serialvolume/etcd imports
// 		github.com/coreos/etcd/clientv3 tested by
// 		github.com/coreos/etcd/clientv3.test imports
// 		github.com/coreos/etcd/integration imports
// 		github.com/coreos/etcd/proxy/grpcproxy imports
// 		google.golang.org/grpc/naming: module google.golang.org/grpc@latest found (v1.44.0, replaced by google.golang.org/grpc@v1.41.0), but does not contain package google.golang.org/grpc/naming

// startMockServer is strongly based on the function of the equivalent name in
// the github.com/rexray/gocsi/testing package. It creates a mock CSI storage
// provider which can be used for testing. name indicates the name of the mock,
// which allows for testing situation with multiple CSI drivers installed.
//
// returns a client connection to a a new mock CSI provider, and a cancel
// function to call when cleaing up.
//func startMockServer(ctx context.Context, name string) (*grpc.ClientConn, func()) {
//	sp := provider.New()
//	// the memconn package lets us avoid the use of a unix domain socket, which
//	// helps make the tests more portable.
//	lis, err := memconn.Listen("memu", fmt.Sprintf("csi-test-%v", name))
//	Expect(err).To(BeNil())
//
//	go func() {
//		defer GinkgoRecover()
//		if err := sp.Serve(ctx, lis); err != nil {
//			Expect(err).To(Equal("http: Server closed"))
//		}
//	}()
//
//	clientOpts := []grpc.DialOption{
//		grpc.WithInsecure(),
//		grpc.WithDialer(func(string, time.Duration) (net.Conn, error) {
//			return memconn.Dial("memu", fmt.Sprintf("csi-test-%v", name))
//		}),
//	}
//
//	client, err := grpc.DialContext(ctx, "", clientOpts...)
//	Expect(err).ToNot(HaveOccurred())
//
//	return client, func() {
//		lis.Close()
//		sp.GracefulStop(ctx)
//	}
//}
