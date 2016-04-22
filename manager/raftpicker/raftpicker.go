package raftpicker

import (
	"sync"

	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/transport"
)

// Picker always picks address of cluster leader.
type Picker struct {
	mu   sync.Mutex
	addr string
	raft AddrSelector
	conn *grpc.Conn
	cc   *grpc.ClientConn
}

// New returns new Picker with AddrSelector interface which it'll use for
// picking. initAddr should be same as for Dial, because there is no way to get
// target from ClientConn.
func New(raft AddrSelector, initAddr string) grpc.Picker {
	return &Picker{raft: raft, addr: initAddr}
}

// Init does initial processing for the Picker, e.g., initiate some connections.
func (p *Picker) Init(cc *grpc.ClientConn) error {
	p.cc = cc
	return nil
}

func (p *Picker) initConn() error {
	if p.conn == nil {
		conn, err := grpc.NewConn(p.cc)
		if err != nil {
			return err
		}
		p.conn = conn
	}
	return nil
}

// Pick blocks until either a transport.ClientTransport is ready for the upcoming RPC
// or some error happens.
func (p *Picker) Pick(ctx context.Context) (transport.ClientTransport, error) {
	p.mu.Lock()
	if err := p.initConn(); err != nil {
		p.mu.Unlock()
		return nil, err
	}
	p.mu.Unlock()

	addr, err := p.raft.LeaderAddr()
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	if p.addr != addr {
		p.addr = addr
		p.conn.NotifyReset()
	}
	p.mu.Unlock()
	return p.conn.Wait(ctx)
}

// PickAddr picks a peer address for connecting. This will be called repeated for
// connecting/reconnecting.
func (p *Picker) PickAddr() (string, error) {
	addr, err := p.raft.LeaderAddr()
	if err != nil {
		return "", err
	}
	p.mu.Lock()
	p.addr = addr
	p.mu.Unlock()
	return addr, nil
}

// State returns the connectivity state of the underlying connections.
func (p *Picker) State() (grpc.ConnectivityState, error) {
	return p.conn.State(), nil
}

// WaitForStateChange blocks until the state changes to something other than
// the sourceState. It returns the new state or error.
func (p *Picker) WaitForStateChange(ctx context.Context, sourceState grpc.ConnectivityState) (grpc.ConnectivityState, error) {
	return p.conn.WaitForStateChange(ctx, sourceState)
}

// Reset the current connection and force a reconnect to another address.
func (p *Picker) Reset() error {
	p.conn.NotifyReset()
	return nil
}

// Close closes all the Conn's owned by this Picker.
func (p *Picker) Close() error {
	return p.conn.Close()
}

// Dial returns *grpc.ClientConn with picker set to raftpicker with initial
// address of cluster leader.
func Dial(selector AddrSelector, opts ...grpc.DialOption) (*grpc.ClientConn, error) {
	addr, err := selector.LeaderAddr()
	if err != nil {
		return nil, err
	}
	picker := New(selector, addr)
	opts = append(opts, grpc.WithPicker(picker))
	return grpc.Dial(addr, opts...)
}
