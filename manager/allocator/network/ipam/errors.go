package ipam

import (
	"fmt"
)

// ErrNetworkNotAllocated is an error type returned when some attachment or
// VIP relies on a network that is not yet allocated. If this error occurs
// during allocation, then allocation should be retried after the network is
// allocated. However, if it occurs during restore, it indicates that the state
// of the raft store is inconsistent
type ErrNetworkNotAllocated struct {
	nwid string
}

// Error returns a formatted error string explaining which network is not
// allocated
func (e ErrNetworkNotAllocated) Error() string {
	return fmt.Sprintf("network %v is not allocated", e.nwid)
}

// IsErrNetworkNotAllocated returns true if the type of the error is
// ErrNetworkNotAllocated
func IsErrNetworkNotAllocated(e error) bool {
	_, ok := e.(ErrNetworkNotAllocated)
	return ok
}

// ErrInvalidIPAM is an error type returned if a given IPAM driver is invalid.
type ErrInvalidIPAM struct {
	ipam string
	nwid string
}

// Error returns a formatted error string explaining which IPAM driver is not
// valid
func (e ErrInvalidIPAM) Error() string {
	return fmt.Sprintf("ipam driver %v for network %v is not valid", e.ipam, e.nwid)
}

// IsErrInvalidIPAM returns true if the type of the error is ErrInvalidIPAM.
func IsErrInvalidIPAM(e error) bool {
	_, ok := e.(ErrInvalidIPAM)
	return ok
}

// ErrBustedIPAM is the error type returned when an IPAM driver operation we
// should expect to succeed fails. It contains the network ID, the IPAM driver
// name, and the error that the IPAM driver returned.
//
// ErrBustedIPAM is different from ErrBadState; ErrBadState indicates that
// something about our current state has caused a failure. ErrBustedIPAM is
// the fault of the ipam driver, nothing wrong with our state.
type ErrBustedIPAM struct {
	ipam string
	nwid string
	err  error
}

// Error returns the formatted error message explaining what broke and why.
func (e ErrBustedIPAM) Error() string {
	return fmt.Sprintf("ipam error from driver %v on network %v: %v", e.ipam, e.nwid, e.err)
}

// IsErrBustedIPAM returns true if the type of the error is ErrBustedIPAM
func IsErrBustedIPAM(e error) bool {
	_, ok := e.(ErrBustedIPAM)
	return ok
}

// ErrInvalidAddress is an error type indicating that a requested address's
// string form is not a valid address and it cannot be parsed.
type ErrInvalidAddress struct {
	address string
}

// Error returns a formatted error message explaining which address is invalid
func (e ErrInvalidAddress) Error() string {
	return fmt.Sprintf("address %v is not a valid IP address", e.address)
}

// IsErrInvalidAddress returns true if the type of the error is
// ErrInvalidAddress.
func IsErrInvalidAddress(e error) bool {
	_, ok := e.(ErrInvalidAddress)
	return ok
}

// ErrBadState is the error type returned when restoring the state of the
// cluster to the local state encounters a problem that is likely a result of
// inconsistent state. It includes the network id and a description of the
// problem
type ErrBadState struct {
	nwid    string
	problem string
}

// Error returns a formatted error string explaining what went wrong and with
// what network
func (e ErrBadState) Error() string {
	return fmt.Sprintf("restoring ipam for network %v encountered a bad state: %v", e.nwid, e.problem)
}

// IsErrBadState returns true if the type of the error is ErrBadState
func IsErrBadState(e error) bool {
	_, ok := e.(ErrBadState)
	return ok
}

// ErrNetworkAllocated is an error type returned when a user tries to call
// AllocateNetwork with a network that has already been allocated, because
// network updates are not supported.
type ErrNetworkAllocated struct {
	nwid string
}

// Error returns a formatted error message explaining that the network is
// already allocated and cannot be updated
func (e ErrNetworkAllocated) Error() string {
	return fmt.Sprintf(
		"network %v is already allocated and network updates are not supported",
		e.nwid,
	)
}

// IsErrNetworkAllocated returns true if the type of the error is
// ErrNetworkAllocated
func IsErrNetworkAllocated(e error) bool {
	_, ok := e.(ErrNetworkAllocated)
	return ok
}

// ErrFailedPoolRequest is returned if requesting an IPAM pool fails
type ErrFailedPoolRequest struct {
	subnet, iprange string
	err             error
}

// Error prints an error message explaining why the pool request failed
func (e ErrFailedPoolRequest) Error() string {
	return fmt.Sprintf(
		"requesting pool (subnet: %q, range: %q) returned error: %v",
		e.subnet, e.iprange, e.err,
	)
}

// IsErrFailedPoolRequest returns true if the type of the error is ErrInvalidPool
func IsErrFailedPoolRequest(e error) bool {
	_, ok := e.(ErrFailedPoolRequest)
	return ok
}

// ErrFailedAddressRequest is the error type used if an address request to the
// IPAM, either for a gateway, an endpoint, or an attachment, fails for any
// reason.
type ErrFailedAddressRequest struct {
	address string
	err     error
}

// Error returns a formatted message explaining which address failed and why
func (e ErrFailedAddressRequest) Error() string {
	return fmt.Sprintf("requesting address %v failed: %v", e.address, e.err)
}

// IsErrFailedAddressRequest returns true if the type of the error is
// ErrFailedAddressRequest
func IsErrFailedAddressRequest(e error) bool {
	_, ok := e.(ErrFailedAddressRequest)
	return ok
}

// ErrDoubleFault indicates that some error occurred while handling another
// error. For example, if allocation of an entire object fails, and then
// rolling back the already allocated bits of the object also fail, then
// ErrDoubleFault will be returned.
type ErrDoubleFault struct {
	original, new error
}

// Error returns a formatted string explaining the original error and the new
// one
func (e ErrDoubleFault) Error() string {
	return fmt.Sprintf("double fault: an error occurred while handling error %v: %v", e.original, e.new)
}

// Original returns the original error that started the error handling logic
func (e ErrDoubleFault) Original() error {
	return e.original
}

// New returns the error that occurred in the error handling logic
func (e ErrDoubleFault) New() error {
	return e.new
}

// IsErrDoubleFault returns true if the type of the error is ErrDoubleFault
func IsErrDoubleFault(e error) bool {
	_, ok := e.(ErrDoubleFault)
	return ok
}
