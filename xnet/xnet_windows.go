//go:build windows

package xnet

import (
	"context"
	"net"

	"github.com/Microsoft/go-winio"
)

// ListenLocal opens a local socket for control communication
func ListenLocal(socket string) (net.Listener, error) {
	// set up ACL for the named pipe
	// allow Administrators and SYSTEM
	sddl := "D:P(A;;GA;;;BA)(A;;GA;;;SY)"
	c := winio.PipeConfig{
		SecurityDescriptor: sddl,
		MessageMode:        true,  // Use message mode so that CloseWrite() is supported
		InputBufferSize:    65536, // Use 64KB buffers to improve performance
		OutputBufferSize:   65536,
	}
	// on windows, our socket is actually a named pipe
	return winio.ListenPipe(socket, &c)
}

// DialContextLocal connects to a local socket or named pipe,
// respecting the context's cancellation and deadline.
func DialContextLocal(ctx context.Context, socket string) (net.Conn, error) {
	return winio.DialPipeContext(ctx, socket)
}
