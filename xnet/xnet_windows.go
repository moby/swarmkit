// +build windows

package xnet

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// ListenLocal opens a local socket for control communication
func ListenLocal(socket string) (net.Listener, error) {
	// on windows, our socket is actually a named pipe
	return winio.ListenPipe(socket, nil)
}

// DialTimeoutLocal is a DialTimeout function for local sockets
func DialTimeoutLocal(socket string, timeout time.Duration) (net.Conn, error) {
	// On windows, we dial a named pipe
	return winio.DialPipe(socket, &timeout)
}
