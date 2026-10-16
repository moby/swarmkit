package xnet

import (
	"context"
	"net"
	"time"
)

// DialTimeoutLocal connects to a local socket or named pipe with the given timeout.
//
// Deprecated: use [DialContextLocal].
func DialTimeoutLocal(socket string, timeout time.Duration) (net.Conn, error) {
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	return DialContextLocal(ctx, socket)
}
