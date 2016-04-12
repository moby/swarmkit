package state

import (
	"sync"
	"time"

	managerpb "github.com/docker/swarm-v2/pb/docker/cluster/api/manager"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/transport"
)

// Raft represents a connection to a raft member
type Raft struct {
	managerpb.RaftClient
	Conn *grpc.ClientConn
}

// GetRaftClient returns a raft client object to communicate
// with other raft members
func GetRaftClient(addr string, timeout time.Duration) (*Raft, error) {
	conn, err := dial(addr, "tcp", timeout)
	if err != nil {
		return nil, err
	}

	return &Raft{
		// TODO(stevvooe): Having raft in the manager stack makes no sense.
		// Need to fix this in another refactor as it is too big of a change.
		RaftClient: managerpb.NewRaftClient(conn),
		Conn:       conn,
	}, nil
}

// dial returns a grpc client connection
func dial(addr string, protocol string, timeout time.Duration) (*grpc.ClientConn, error) {
	grpcOptions := []grpc.DialOption{grpc.WithInsecure(), grpc.WithPicker(&reconnectPicker{target: addr})}
	if timeout != 0 {
		grpcOptions = append(grpcOptions, grpc.WithTimeout(timeout))
	}
	return grpc.Dial(addr, grpcOptions...)
}

// Register registers the node raft server
func Register(server *grpc.Server, node *Node) {
	managerpb.RegisterRaftServer(server, node)
}

// reconnectPicker is a Picker which attempts a new connection if necessary
// before each request. It's used to work around GRPC's exponential backoff,
// which is undesired for raft.
type reconnectPicker struct {
	target string
	conn   *grpc.Conn
	cc     *grpc.ClientConn

	mu sync.Mutex
}

func (p *reconnectPicker) Init(cc *grpc.ClientConn) error {
	// Init does not need to hold the mutex, because it's either being
	// called from Dial before anything else can use the picker, or from
	// Pick, which holds the mutex.

	p.cc = cc
	c, err := grpc.NewConn(cc)
	if err != nil {
		return err
	}
	p.conn = c
	return nil
}

func (p *reconnectPicker) Pick(ctx context.Context) (transport.ClientTransport, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// TODO(aaronl): This is a very poor way of triggering a new connection
	// attempt. We really need some way of telling the existing p.conn to
	// try again. Unfortunately, NotifyReset doesn't seem to do anything
	// immediate when a connection is in its retry cycle.
	if p.conn.State() != grpc.Ready {
		p.conn.Close()
		p.Init(p.cc)
	}
	return p.conn.Wait(ctx)
}

func (p *reconnectPicker) PickAddr() (string, error) {
	return p.target, nil
}

func (p *reconnectPicker) State() (grpc.ConnectivityState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.conn.State(), nil
}

func (p *reconnectPicker) WaitForStateChange(ctx context.Context, sourceState grpc.ConnectivityState) (grpc.ConnectivityState, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.conn.WaitForStateChange(ctx, sourceState)
}

func (p *reconnectPicker) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}
