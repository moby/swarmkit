package driver

// TODO(dperny) write

// The drivers package contains the Allocator in charge of setting a Network's
// Driver state.
//
// The drivers package owns the following fields, and should be the only
// component that writes to them:
//
// - api.Network.Driver
