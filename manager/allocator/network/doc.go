package network

// The allocator/network package is the subcomponent in charge of allocating
// network resources for swarmkit resources. Give it objects, and it gives you
// resources.
//
// Th network allocator provides, essentially, an interface between swarmkit
// objects and the underlying libnetwork primatives. It carries a lot of local
// state by consequence of the fact that the underlying primatives are
// local-only and need to be initialized each time they are used.
//
// In swarmkit you'll see two kinds of objects: components with an event loop,
// and components which are strictly accessed through method calls. This is the
// latter.
//
// The network allocator doesn't have direct access to the raft store. The
// caller will provide all of the objects, and be in charge of committing those
// objects to the raft store. Instead, it works directly on the api objects,
// and gives them back to the caller to figure out how to store. This frees us
// from having to worry about the overlying storage method, and reduces the
// already rather bloated surface are of this component
//
// The network package fully owns a few fields, and should be the _only_
// component that ever writes to them. Conversely, these are the _only_ fields
// that the Allocator ever writes to, meaning it can safely work on an object
// concurrently with other routines, (as long as they don't try to read these
// fields) and the fields it owns can be merged into objects safely. These
// fields are:
//
// - api.Network:
//   - DriverState
//   - IPAM
// - api.Node:
//   - Attachment
//   - Attachments
// - api.Service
//   - Endpoint
// - api.Task
//   - Networks
//   - Endpoint
//
// These are essentially all of the objects of types:
// - api.Driver
// - api.IPAMOptions
// - api.NetworkAttachment
// - api.Endpoint
//
// In addition, these objects are treated internally as "atomic". They're
// either allcoated or not. A higher-level object like a Service or Task might
// be partially allocated, with only some of the NetworkAttachments or
// Endpoints matching the desired state, but each individual NetworkAttachment
// and Endpoint will either be completely allocated or not allocated at all.
// Treating these objects this way avoids the difficult problem of handling a
// bunch of partial-allocation edge-cases.
