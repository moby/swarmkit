// Package connectionbroker is a layer on top of remotes that returns
// a gRPC connection to a manager. The connection may be a local connection
// using a local socket such as a UNIX socket.
package connectionbroker

import (
	"net"
	"sync"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/remotes"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
)

// Broker is a simple connection broker. It can either return a fresh
// connection to a remote manager selected with weighted randomization, or a
// local gRPC connection to the local manager.
type Broker struct {
	mu                 sync.Mutex
	remotes            remotes.Remotes
	localConn          *grpc.ClientConn
	defaultDialOptions []grpc.DialOption
}

// New creates a new connection broker.
func New(remotes remotes.Remotes, opts ...grpc.DialOption) *Broker {
	return &Broker{
		remotes:            remotes,
		defaultDialOptions: opts,
	}
}

// SetLocalConn changes the local gRPC connection used by the connection broker.
func (b *Broker) SetLocalConn(address string, dialOptions ...grpc.DialOption) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	var localConn *grpc.ClientConn
	if len(address) > 0 {
		// Adding the default dial options
		dialOptions = append(dialOptions, b.defaultDialOptions...)

		var err error
		localConn, err = grpc.Dial(address, dialOptions...)
		if err != nil {
			return err
		}
	}

	b.localConn = localConn
	return nil
}

// GetLocalConn returns the broker local connection
func (b *Broker) GetLocalConn() *grpc.ClientConn {
	return b.localConn
}

// Select a manager from the set of available managers, and return a connection.
func (b *Broker) Select(dialOpts ...grpc.DialOption) (*Conn, error) {
	b.mu.Lock()
	localConn := b.localConn
	b.mu.Unlock()

	if localConn != nil {
		return &Conn{
			ClientConn: localConn,
			isLocal:    true,
		}, nil
	}

	return b.SelectRemote(dialOpts...)
}

// SelectRemote chooses a manager from the remotes, and returns a TCP
// connection.
func (b *Broker) SelectRemote(dialOpts ...grpc.DialOption) (*Conn, error) {
	peer, err := b.remotes.Select()

	if err != nil {
		return nil, err
	}

	// Adding the default dial options
	dialOpts = append(dialOpts, b.defaultDialOptions...)

	// gRPC dialer connects to proxy first. Provide a custom dialer here avoid that.
	// TODO(anshul) Add an option to configure this.
	dialOpts = append(dialOpts,
		grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithDialer(func(addr string, timeout time.Duration) (net.Conn, error) {
			return net.DialTimeout("tcp", addr, timeout)
		}))

	cc, err := grpc.Dial(peer.Addr, dialOpts...)
	if err != nil {
		b.remotes.ObserveIfExists(peer, -remotes.DefaultObservationWeight)
		return nil, err
	}

	return &Conn{
		ClientConn: cc,
		remotes:    b.remotes,
		peer:       peer,
	}, nil
}

// Remotes returns the remotes interface used by the broker, so the caller
// can make observations or see weights directly.
func (b *Broker) Remotes() remotes.Remotes {
	return b.remotes
}

// Conn is a wrapper around a gRPC client connection.
type Conn struct {
	*grpc.ClientConn
	isLocal bool
	remotes remotes.Remotes
	peer    api.Peer
}

// Peer returns the peer for this Conn.
func (c *Conn) Peer() api.Peer {
	return c.peer
}

// Close closes the client connection if it is a remote connection. It also
// records a positive experience with the remote peer if success is true,
// otherwise it records a negative experience. If a local connection is in use,
// Close is a noop.
func (c *Conn) Close(success bool) error {
	if c.isLocal {
		return nil
	}

	if success {
		c.remotes.ObserveIfExists(c.peer, remotes.DefaultObservationWeight)
	} else {
		c.remotes.ObserveIfExists(c.peer, -remotes.DefaultObservationWeight)
	}

	return c.ClientConn.Close()
}
