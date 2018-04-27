package errors

import (
	"github.com/stretchr/testify/require"
	"testing"
)

// The tests in this file are mostly boilerplate, and their purpose is
// essentially to catch typos or dumb errors, and to get higher test coverage
// so this package doesn't artificially drag down the coverage. It also is
// useful to see the error messages written out as string to verify that after
// formatting they don't look dumb
//
// And if you ever think writing tests for such trivial code, these tests have
// caught:
//
// - an error invovling a failure to expand variadic arguments
//   (using `args` instead of `args...`)
// - an error involving a failure to pass args to string formatting
//   (fmt.Sprintf("something %v") instead of fmt.Sprintf("something %v", arg)

func TestErrAlreadyAllocated(t *testing.T) {
	t.Parallel()

	err := ErrAlreadyAllocated()
	require.EqualError(t, err, "object is already fully allocated")
	require.True(t, IsErrAlreadyAllocated(err))
}

func TestErrDependencyNotAllocated(t *testing.T) {
	t.Parallel()

	err := ErrDependencyNotAllocated("network", "someid")
	require.EqualError(t, err, "network someid depended on by object is not allocated")
	require.True(t, IsErrDependencyNotAllocated(err))
}

func TestErrInvalidSpec(t *testing.T) {
	t.Parallel()

	// with string formatting
	err := ErrInvalidSpec("something is %q", "wrong")
	require.EqualError(t, err, "spec is invalid: something is \"wrong\"")
	require.True(t, IsErrInvalidSpec(err))

	// without string formatting
	err2 := ErrInvalidSpec("foo")
	require.EqualError(t, err2, "spec is invalid: foo")
	require.True(t, IsErrInvalidSpec(err2))

}

func TestErrInternal(t *testing.T) {
	t.Parallel()

	// with string formatting
	err := ErrInternal("something is %v %v", "wrong", 1234)
	require.EqualError(t, err, "internal allocator error: something is wrong 1234")
	require.True(t, IsErrInternal(err))

	// without string formatting
	err2 := ErrInternal("foo")
	require.EqualError(t, err2, "internal allocator error: foo")
	require.True(t, IsErrInternal(err2))
}

func TestErrResourceExhausted(t *testing.T) {
	t.Parallel()

	// with string formatting
	err := ErrResourceExhausted("ip address", "pool %v is %v", 12345, "full")
	require.EqualError(t, err, "resource ip address is exhausted: pool 12345 is full")
	require.True(t, IsErrResourceExhausted(err))

	// without string formatting
	err2 := ErrResourceExhausted("port", "nothing remains")
	require.EqualError(t, err2, "resource port is exhausted: nothing remains")
	require.True(t, IsErrResourceExhausted(err2))
}

func TestErrResourceInUse(t *testing.T) {
	t.Parallel()

	err := ErrResourceInUse("ip address", "192.168.187.1")
	require.EqualError(t, err, "ip address 192.168.187.1 is in use")
	require.True(t, IsErrResourceInUse(err))
}

func TestErrBadState(t *testing.T) {
	t.Parallel()

	// with string formatting
	err := ErrBadState("something wrong %v %v", "in the", "allocator")
	require.EqualError(t, err, "an invalid state was encountered: something wrong in the allocator")
	require.True(t, IsErrBadState(err))

	// without string formatting
	err2 := ErrBadState("totally busted")
	require.EqualError(t, err2, "an invalid state was encountered: totally busted")
	require.True(t, IsErrBadState(err2))
}
