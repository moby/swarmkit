package agent

import "golang.org/x/net/context"

// modificationRequest drives the task manager allowing serialized access to
// the task manager instance. Use with modify to queue and dispatch operations.

// modificationRequest defines an operation that can be run in the context of a
// select statement.
type modificationRequest struct {
	// fn defines the operation to run within the protected context of the
	// target select statement.
	//
	// A deferred function can be return that will be run after the request,
	// but still while holding the "lock". This is useful if an operation needs
	// to be performed that shouldn't block the caller.
	fn       func(ctx context.Context) (deferred func(), err error)
	response chan error
}

// modify queues the operation on requestq and waits for it to complete. If
// wait is closed, waitErr will be returned. Otherwise, it will wait for fn to
// be called and an error to returned as a result.
func modify(ctx context.Context, requestq chan modificationRequest, wait chan struct{}, waitErr error, fn func(ctx context.Context) (deferred func(), err error)) error {
	response := make(chan error, 1)

	select {
	case requestq <- modificationRequest{
		fn:       fn,
		response: response,
	}:
		select {
		case err := <-response:
			return err
		case <-wait:
			return waitErr
		case <-ctx.Done():
			return ctx.Err()
		}
	case <-wait:
		return waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}
