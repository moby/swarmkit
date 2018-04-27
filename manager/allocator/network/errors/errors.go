package errors

import "fmt"

type errAlreadyAllocated struct{}

// ErrAlreadyAllocated creates an error indicating that the object Allocate was
// called on is already fully allocated.
func ErrAlreadyAllocated() error {
	return errAlreadyAllocated{}
}

// Error returns the error message
func (e errAlreadyAllocated) Error() string {
	return "object is already fully allocated"
}

// IsErrAlreadyAllocated returns true if this error is a result of trying to
// allocate an object already allocated
func IsErrAlreadyAllocated(e error) bool {
	_, ok := e.(errAlreadyAllocated)
	return ok
}

type errDependencyNotAllocated struct {
	objectType string
	id         string
}

// ErrDependencyNotAllocated creates an error indicating that we're trying to
// allocate a service or task that depends on a network or service not
// allocated yet.
func ErrDependencyNotAllocated(objectType string, id string) error {
	return errDependencyNotAllocated{
		objectType: objectType,
		id:         id,
	}
}

// Error returns the error message
func (e errDependencyNotAllocated) Error() string {
	return fmt.Sprintf("%v %v depended on by object is not allocated", e.objectType, e.id)
}

// IsErrDependencyNotAllocated returns true if this error is a result of a
// dependency not being allocated
func IsErrDependencyNotAllocated(e error) bool {
	_, ok := e.(errDependencyNotAllocated)
	return ok
}

type errInvalidSpec struct {
	// cause should be a string explaining what part is invalid. for example,
	cause string
}

// ErrInvalidSpec returns an error indicating that something about a spec is
// invalid and cannot be operated on; for example, if an IP address isn't in an
// acceptable format, or if the driver specified does not exist.
func ErrInvalidSpec(cause string, args ...interface{}) error {
	if len(args) != 0 {
		return errInvalidSpec{cause: fmt.Sprintf(cause, args...)}
	}
	return errInvalidSpec{cause: cause}
}

// Error returns a formatted error message
func (e errInvalidSpec) Error() string {
	return fmt.Sprintf("spec is invalid: %v", e.cause)
}

// IsErrInvalidSpec returns true if this error is a result of an invalid spec
func IsErrInvalidSpec(e error) bool {
	_, ok := e.(errInvalidSpec)
	return ok
}

type errInternal struct {
	cause string
}

// ErrInternal creates an error type indicating that some internal component we
// expect to succeed fails. For example, deallocating an IP address should
// never fail, but there is still an error type returned.  Note that failures
// of remote driver components, like if the user is using a plugin, will also
// cause ErrInternal, so this doesn't necessarily mean the problem isn't the
// user's fault. This catchall error allows every error type returned by network
// allocator components to be backed by a concrete unambiguous type.
func ErrInternal(cause string, args ...interface{}) error {
	if len(args) != 0 {
		return errInternal{cause: fmt.Sprintf(cause, args...)}
	}
	return errInternal{cause: cause}
}

// Error returns a formatted error message
func (e errInternal) Error() string {
	return fmt.Sprintf("internal allocator error: %v", e.cause)
}

// IsErrInternal returns true if this error is a result of some unexpected
// internal failure
func IsErrInternal(e error) bool {
	_, ok := e.(errInternal)
	return ok
}

type errResourceExhausted struct {
	resource string
	cause    string
}

// ErrResourceExhausted creates an error indicating that a requested resource
// expected to be automatically allocated, like a port or IP address, cannot be
// allocated because there are no free resources matching the requirements of
// the object
func ErrResourceExhausted(resource, cause string, args ...interface{}) error {
	if len(args) != 0 {
		return errResourceExhausted{
			resource: resource,
			cause:    fmt.Sprintf(cause, args...),
		}
	}
	return errResourceExhausted{resource: resource, cause: cause}
}

// Error returns a formatted error message
func (e errResourceExhausted) Error() string {
	return fmt.Sprintf("resource %v is exhausted: %v", e.resource, e.cause)
}

// IsErrResourceExhausted returns true if this error is a result of some
// resource being exhausted
func IsErrResourceExhausted(e error) bool {
	_, ok := e.(errResourceExhausted)
	return ok
}

type errResourceInUse struct {
	resourceType, value string
}

// ErrResourceInUse creates an error indicating that a specifically requested
// resource is already in used.
func ErrResourceInUse(resourceType, value string) error {
	return errResourceInUse{
		resourceType: resourceType,
		value:        value,
	}
}

// Error returns a formatted error message
func (e errResourceInUse) Error() string {
	return fmt.Sprintf("%v %v is in use", e.resourceType, e.value)
}

// IsErrResourceInUse returns true if the error is a result of some requested
// resource being in use by something else.
func IsErrResourceInUse(e error) bool {
	_, ok := e.(errResourceInUse)
	return ok
}

type errBadState struct {
	cause string
}

// ErrBadState creates an error indicating that some internal state of swarmkit
// is inconsistent. For example, if two services have conflicting IP address
// allocation when attempting to restore, this error type will be returned
func ErrBadState(cause string, args ...interface{}) error {
	if len(args) != 0 {
		return errBadState{cause: fmt.Sprintf(cause, args...)}
	}
	return errBadState{cause: cause}
}

// Error returns a formatted error message
func (e errBadState) Error() string {
	return fmt.Sprintf("an invalid state was encountered: %v", e.cause)
}

// IsErrBadState returns true if this error is a result of bad internal state
func IsErrBadState(e error) bool {
	_, ok := e.(errBadState)
	return ok
}
