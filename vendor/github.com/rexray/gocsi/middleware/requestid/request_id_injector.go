package requestid

import (
	"fmt"
	"strconv"
	"sync/atomic"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"

	csictx "github.com/rexray/gocsi/context"
)

type interceptor struct {
	id uint64
}

// NewServerRequestIDInjector returns a new UnaryServerInterceptor
// that reads a unique request ID from the incoming context's gRPC
// metadata. If the incoming context does not contain gRPC metadata or
// a request ID, then a new request ID is generated.
func NewServerRequestIDInjector() grpc.UnaryServerInterceptor {
	return newRequestIDInjector().handleServer
}

// NewClientRequestIDInjector provides a UnaryClientInterceptor
// that injects the outgoing context with gRPC metadata that contains
// a unique ID.
func NewClientRequestIDInjector() grpc.UnaryClientInterceptor {
	return newRequestIDInjector().handleClient
}

func newRequestIDInjector() *interceptor {
	return &interceptor{}
}

func (s *interceptor) handleServer(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	// storeID is a flag that indicates whether or not the request ID
	// should be atomically stored in the interceptor's id field at
	// the end of this function. If the ID was found in the incoming
	// request and could be parsed successfully then the ID is stored.
	// If the ID was generated server-side then the ID is not stored.
	storeID := true

	// Retrieve the gRPC metadata from the incoming context.
	md, mdOK := metadata.FromIncomingContext(ctx)

	// If no gRPC metadata was found then create some and ensure the
	// context is a gRPC incoming context.
	if !mdOK {
		md = metadata.Pairs()
		ctx = metadata.NewIncomingContext(ctx, md)
	}

	// Check the metadata from the request ID.
	szID, szIDOK := md[csictx.RequestIDKey]

	// If the metadata does not contain a request ID then create a new
	// request ID and inject it into the metadata.
	if !szIDOK || len(szID) != 1 {
		szID = []string{fmt.Sprintf("%d", atomic.AddUint64(&s.id, 1))}
		md[csictx.RequestIDKey] = szID
		storeID = false
	}

	// Parse the request ID from the
	id, err := strconv.ParseUint(szID[0], 10, 64)
	if err != nil {
		id = atomic.AddUint64(&s.id, 1)
		storeID = false
	}

	if storeID {
		atomic.StoreUint64(&s.id, id)
	}

	return handler(ctx, req)
}

func (s *interceptor) handleClient(
	ctx context.Context,
	method string,
	req, rep interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {

	// Ensure there is an outgoing gRPC context with metadata.
	md, mdOK := metadata.FromOutgoingContext(ctx)
	if !mdOK {
		md = metadata.Pairs()
		ctx = metadata.NewOutgoingContext(ctx, md)
	}

	// Ensure the request ID is set in the metadata.
	if szID, szIDOK := md[csictx.RequestIDKey]; !szIDOK || len(szID) != 1 {
		szID = []string{fmt.Sprintf("%d", atomic.AddUint64(&s.id, 1))}
		md[csictx.RequestIDKey] = szID
	}

	return invoker(ctx, method, req, rep, cc, opts...)
}
