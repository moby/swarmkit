// +build !windows

package xnet

import (
	"net"
	"time"
)

// Listen Wrapper for net.Listen
func Listen(proto string, addr string) (net.Listener, error) {
	return net.Listen(proto, addr)
}

// DialTimeout Wrapper for net.Listen
func DialTimeout(proto string, addr string, timeout time.Duration) (net.Conn, error) {
	return net.DialTimeout(proto, addr, timeout)
}
