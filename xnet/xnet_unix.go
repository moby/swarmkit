//go:build !windows

package xnet

import (
	"context"
	"net"
)

// ListenLocal opens a local socket for control communication
func ListenLocal(socket string) (net.Listener, error) {
	// on unix it's just a unix socket
	return net.Listen("unix", socket)
}

// DialContextLocal connects to a local socket using the provided context.
// On Unix, it connects to a Unix domain socket; on Windows, to a named pipe.
func DialContextLocal(ctx context.Context, socket string) (net.Conn, error) {
	return (&net.Dialer{}).DialContext(ctx, "unix", socket)
}
