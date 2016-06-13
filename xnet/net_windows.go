// +build windows

package xnet

import (
	"net"
	"time"

	"github.com/Microsoft/go-winio"
)

// Listen Wrapper for net.Listen and Windows named pipe
func Listen(proto string, addr string) (net.Listener, error) {
	if proto == "npipe" {
		return winio.ListenPipe(addr, "")
	}
	return net.Listen(proto, addr)
}

// DialTimeout Wrapper for net.DialTimeout and Windows named pipe (winio.DialPipe)
func DialTimeout(proto string, addr string, timeout time.Duration) (net.Conn, error) {
	if proto == "npipe" {
		return winio.DialPipe(addr, &timeout)
	}
	return net.DialTimeout(proto, addr, timeout)
}
