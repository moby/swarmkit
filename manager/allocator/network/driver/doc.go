package driver

// The drivers package contains the Allocator in charge of setting a Network's
// Driver state. Network drivers can have one of two scopes. A local scoped
// driver does not need swarmkit allocation. A global scoped driver does. The
// reality is more complex than that, but those are the only differences
// swarmkit cares about.  Before a network can be used, if it depends on a
// global-scoped driver, it must be allocated on that driver. This package
// provides the code that handles that state allocation.
//
// The drivers package owns the following fields, and should be the only
// component that writes to them:
//
// - api.Network.Driver
