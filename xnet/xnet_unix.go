//go:build !windows

package xnet

import (
	"context"
	"net"
	"time"
)

// ListenLocal opens a local socket for control communication
func ListenLocal(socket string) (net.Listener, error) {
	// on unix it's just a unix socket
	return (&net.ListenConfig{}).Listen(context.Background(), "unix", socket)
}

// DialTimeoutLocal is a DialTimeout function for local sockets
func DialTimeoutLocal(socket string, timeout time.Duration) (net.Conn, error) {
	// on unix, we dial a unix socket
	return (&net.Dialer{Timeout: timeout}).DialContext(context.Background(), "unix", socket)
}
