package raftpicker

import (
	"golang.org/x/net/context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/transport"
)

// Picker always picks address of cluster leader.
type Picker struct {
	raft AddrSelector
	conn *grpc.Conn
}

// New returns new Picker with AddrSelector interface which it'll use for
// picking.
func New(raft AddrSelector) grpc.Picker {
	return &Picker{raft: raft}
}

// Init does initial processing for the Picker, e.g., initiate some connections.
func (p *Picker) Init(cc *grpc.ClientConn) error {
	c, err := grpc.NewConn(cc)
	if err != nil {
		return err
	}
	p.conn = c
	return nil
}

// Pick blocks until either a transport.ClientTransport is ready for the upcoming RPC
// or some error happens.
func (p *Picker) Pick(ctx context.Context) (transport.ClientTransport, error) {
	return p.conn.Wait(ctx)
}

// PickAddr picks a peer address for connecting. This will be called repeated for
// connecting/reconnecting.
func (p *Picker) PickAddr() (string, error) {
	return p.raft.LeaderAddr()
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
