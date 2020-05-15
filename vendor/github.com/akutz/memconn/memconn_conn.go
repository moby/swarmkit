package memconn

import (
	"bytes"
	"fmt"
	"net"
	"sync"
	"time"
)

// Conn is an in-memory implementation of Golang's "net.Conn" interface.
type Conn struct {
	pipe

	laddr Addr
	raddr Addr

	// buf contains information about the connection's buffer state if
	// the connection is buffered. Otherwise this field is nil.
	buf *bufConn
}

type bufConn struct {

	// writeMu prevents concurrent, buffered writes
	writeMu sync.Mutex

	// Please see the SetBufferSize function for more information.
	max int

	// Please see the SetCloseTimeout function for more information.
	closeTimeout time.Duration

	// configMu is used to synchronize access to buffered connection
	// settings.
	configMu sync.RWMutex

	// errs is the error channel returned by the Errs() function and
	// used to report erros that occur as a result of buffered write
	// operations. If the pipe does not use buffered writes then this
	// field will always be nil.
	errs chan error

	// data is a circular buffer used to provide buffered writes
	data bytes.Buffer

	// dataN is a FIFO list of the n bytes written to data
	dataN []int

	// dataMu guards access to data and dataN
	dataMu sync.Mutex
}

func makeNewConns(network string, laddr, raddr Addr) (*Conn, *Conn) {
	// This code is duplicated from the Pipe() function from the file
	// "memconn_pipe.go". The reason for the duplication is to optimize
	// the performance by removing the need to wrap the *pipe values as
	// interface{} objects out of the Pipe() function and assert them
	// back as *pipe* objects in this function.
	cb1 := make(chan []byte)
	cb2 := make(chan []byte)
	cn1 := make(chan int)
	cn2 := make(chan int)
	done1 := make(chan struct{})
	done2 := make(chan struct{})

	// Wrap the pipes with Conn to support:
	//
	//   * The correct address information for the functions LocalAddr()
	//     and RemoteAddr() return the
	//   * Errors returns from the internal pipe are checked and
	//     have their internal OpError addr information replaced with
	//     the correct address information.
	//   * A channel can be setup to cause the event of the Listener
	//     closing closes the remoteConn immediately.
	//   * Buffered writes
	local := &Conn{
		pipe: pipe{
			rdRx: cb1, rdTx: cn1,
			wrTx: cb2, wrRx: cn2,
			localDone: done1, remoteDone: done2,
			readDeadline:  makePipeDeadline(),
			writeDeadline: makePipeDeadline(),
		},
		laddr: laddr,
		raddr: raddr,
	}
	remote := &Conn{
		pipe: pipe{
			rdRx: cb2, rdTx: cn2,
			wrTx: cb1, wrRx: cn1,
			localDone: done2, remoteDone: done1,
			readDeadline:  makePipeDeadline(),
			writeDeadline: makePipeDeadline(),
		},
		laddr: raddr,
		raddr: laddr,
	}

	if laddr.Buffered() {
		local.buf = &bufConn{
			errs:         make(chan error),
			closeTimeout: 0 * time.Second,
		}
	}

	if raddr.Buffered() {
		remote.buf = &bufConn{
			errs:         make(chan error),
			closeTimeout: 3 * time.Second,
		}
	}

	return local, remote
}

// LocalBuffered returns a flag indicating whether or not the local side
// of the connection is buffered.
func (c *Conn) LocalBuffered() bool {
	return c.laddr.Buffered()
}

// RemoteBuffered returns a flag indicating whether or not the remote side
// of the connection is buffered.
func (c *Conn) RemoteBuffered() bool {
	return c.raddr.Buffered()
}

// BufferSize gets the number of bytes allowed to be queued for
// asynchrnous Write operations.
//
// Please note that this function will always return zero for unbuffered
// connections.
//
// Please see the function SetBufferSize for more information.
func (c *Conn) BufferSize() int {
	if c.laddr.Buffered() {
		c.buf.configMu.RLock()
		defer c.buf.configMu.RUnlock()
		return c.buf.max
	}
	return 0
}

// SetBufferSize sets the number of bytes allowed to be queued for
// asynchronous Write operations. Once the amount of data pending a Write
// operation exceeds the specified size, subsequent Writes will
// block until the queued data no longer exceeds the allowed ceiling.
//
// A value of zero means no maximum is defined.
//
// If a Write operation's payload length exceeds the buffer size
// (except for zero) then the Write operation is handled synchronously.
//
// Please note that setting the buffer size has no effect on unbuffered
// connections.
func (c *Conn) SetBufferSize(i int) {
	if c.laddr.Buffered() {
		c.buf.configMu.Lock()
		defer c.buf.configMu.Unlock()
		c.buf.max = i
	}
}

// CloseTimeout gets the time.Duration value used when closing buffered
// connections.
//
// Please note that this function will always return zero for
// unbuffered connections.
//
// Please see the function SetCloseTimeout for more information.
func (c *Conn) CloseTimeout() time.Duration {
	if c.laddr.Buffered() {
		c.buf.configMu.RLock()
		defer c.buf.configMu.RUnlock()
		return c.buf.closeTimeout
	}
	return 0
}

// SetCloseTimeout sets a time.Duration value used by the Close function
// to determine the amount of time to wait for pending, buffered Writes
// to complete before closing the connection.
//
// The default timeout value is 10 seconds. A zero value does not
// mean there is no timeout, rather it means the timeout is immediate.
//
// Please note that setting this value has no effect on unbuffered
// connections.
func (c *Conn) SetCloseTimeout(d time.Duration) {
	if c.laddr.Buffered() {
		c.buf.configMu.Lock()
		defer c.buf.configMu.Unlock()
		c.buf.closeTimeout = d
	}
}

// LocalAddr implements the net.Conn LocalAddr method.
func (c *Conn) LocalAddr() net.Addr {
	return c.laddr
}

// RemoteAddr implements the net.Conn RemoteAddr method.
func (c *Conn) RemoteAddr() net.Addr {
	return c.raddr
}

// Close implements the net.Conn Close method.
func (c *Conn) Close() error {
	c.pipe.once.Do(func() {

		// Buffered connections will attempt to wait until all
		// pending Writes are completed or until the specified
		// timeout value has elapsed.
		if c.laddr.Buffered() {

			// Set up a channel that is closed when the specified
			// timer elapses.
			timeout := c.CloseTimeout()
			timeoutDone := make(chan struct{})
			if timeout == 0 {
				close(timeoutDone)
			} else {
				time.AfterFunc(timeout, func() { close(timeoutDone) })
			}

			// Set up a channel that is closed when there is
			// no more buffered data.
			writesDone := make(chan struct{})
			go func() {
				c.buf.dataMu.Lock()
				defer c.buf.dataMu.Unlock()
				for len(c.buf.dataN) > 0 {
					c.buf.dataMu.Unlock()
					c.buf.dataMu.Lock()
				}
				close(writesDone)
			}()

			// Wait to close the connection.
			select {
			case <-writesDone:
			case <-timeoutDone:
			}
		}

		close(c.pipe.localDone)
	})
	return nil
}

// Errs returns a channel that receives errors that may occur as the
// result of buffered write operations.
//
// This function will always return nil for unbuffered connections.
//
// Please note that the channel returned by this function is not closed
// when the connection is closed. This is because errors may continue
// to be sent over this channel as the result of asynchronous writes
// occurring after the connection is closed. Therefore this channel
// should not be used to determine when the connection is closed.
func (c *Conn) Errs() <-chan error {
	return c.buf.errs
}

// Read implements the net.Conn Read method.
func (c *Conn) Read(b []byte) (int, error) {
	n, err := c.pipe.Read(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok {
			e.Addr = c.raddr
			e.Source = c.laddr
			return n, e
		}
		return n, &net.OpError{
			Op:     "read",
			Addr:   c.raddr,
			Source: c.laddr,
			Net:    c.raddr.Network(),
			Err:    err,
		}
	}
	return n, nil
}

// Write implements the net.Conn Write method.
func (c *Conn) Write(b []byte) (int, error) {
	if c.laddr.Buffered() {
		return c.writeAsync(b)
	}
	return c.writeSync(b)
}

func (c *Conn) writeSync(b []byte) (int, error) {
	n, err := c.pipe.Write(b)
	if err != nil {
		if e, ok := err.(*net.OpError); ok {
			e.Addr = c.raddr
			e.Source = c.laddr
			return n, e
		}
		return n, &net.OpError{
			Op:     "write",
			Addr:   c.raddr,
			Source: c.laddr,
			Net:    c.raddr.Network(),
			Err:    err,
		}
	}
	return n, nil
}

// writeAsync performs the Write operation in a goroutine. This
// behavior means the Write operation is not blocking, but also means
// that when Write operations fail the associated error is not returned
// from this function.
func (c *Conn) writeAsync(b []byte) (int, error) {
	// Prevent concurrent writes.
	c.buf.writeMu.Lock()
	defer c.buf.writeMu.Unlock()

	// Get the max buffer size to determine if the buffer's capacity
	// should be grown.
	c.buf.configMu.RLock()
	max := c.buf.max
	c.buf.configMu.RUnlock()

	// If the provided data is too large for the buffer then force
	// a synchrnous write.
	if max > 0 && len(b) > max {
		return c.writeSync(b)
	}

	// Lock access to the buffer. This prevents concurrent writes to
	// the buffer. Occasionally the lock is released temporarily to
	// check if the buffer's length allows this write operation to
	// proceed.
	c.buf.dataMu.Lock()

	// If the max buffer size is non-zero and larger than the
	// capacity of the buffer, then grow the buffer capacity.
	if max > 0 && max > c.buf.data.Cap() {
		c.buf.data.Grow(max)
	}

	// Wait until there is room in the buffer to proceed.
	for max > 0 && c.buf.data.Len()+len(b) > c.buf.data.Cap() {
		c.buf.dataMu.Unlock()
		c.buf.dataMu.Lock()
	}
	defer c.buf.dataMu.Unlock()

	// Write the data to the buffer.
	n, err := c.buf.data.Write(b)
	if err != nil {
		return n, err
	} else if n < len(b) {
		return n, fmt.Errorf("trunc write: exp=%d act=%d", len(b), n)
	}

	// Record the number of bytes written in a FIFO list.
	c.buf.dataN = append(c.buf.dataN, n)

	// Start a goroutine that reads n bytes from the buffer where n
	// is the first element in the FIFO list from above. The read
	// below may not actually correspond to the write from above;
	// that's okay. The important thing is the order of the reads,
	// and that's achieved using the circular buffer and FIFO list
	// of bytes written.
	go func() {
		// The read operation must also obtain a lock, preventing
		// concurrent access to the buffer.
		c.buf.dataMu.Lock()

		// Get the number of bytes to read.
		n := c.buf.dataN[0]
		c.buf.dataN = c.buf.dataN[1:]

		// Read the bytes from the buffer into a temporary buffer.
		b := make([]byte, n)
		if nr, err := c.buf.data.Read(b); err != nil {
			go func() { c.buf.errs <- err }()
			c.buf.dataMu.Unlock()
			return
		} else if nr < n {
			go func() {
				c.buf.errs <- fmt.Errorf("trunc read: exp=%d act=%d", n, nr)
			}()
			c.buf.dataMu.Unlock()
			return
		}

		// Ensure access to the buffer is restored.
		defer c.buf.dataMu.Unlock()

		// Write the temporary buffer into the underlying connection.
		if nw, err := c.writeSync(b); err != nil {
			go func() { c.buf.errs <- err }()
			return
		} else if nw < n {
			go func() {
				c.buf.errs <- fmt.Errorf("trunc write: exp=%d act=%d", n, nw)
			}()
			return
		}
	}()

	return n, nil
}

// SetReadDeadline implements the net.Conn SetReadDeadline method.
func (c *Conn) SetReadDeadline(t time.Time) error {
	if err := c.pipe.SetReadDeadline(t); err != nil {
		if e, ok := err.(*net.OpError); ok {
			e.Addr = c.laddr
			e.Source = c.laddr
			return e
		}
		return &net.OpError{
			Op:     "setReadDeadline",
			Addr:   c.laddr,
			Source: c.laddr,
			Net:    c.laddr.Network(),
			Err:    err,
		}
	}
	return nil
}

// SetWriteDeadline implements the net.Conn SetWriteDeadline method.
func (c *Conn) SetWriteDeadline(t time.Time) error {
	if err := c.pipe.SetWriteDeadline(t); err != nil {
		if e, ok := err.(*net.OpError); ok {
			e.Addr = c.laddr
			e.Source = c.laddr
			return e
		}
		return &net.OpError{
			Op:     "setWriteDeadline",
			Addr:   c.laddr,
			Source: c.laddr,
			Net:    c.laddr.Network(),
			Err:    err,
		}
	}
	return nil
}
