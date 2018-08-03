package network

// The allocator/network package is the subcomponent in charge of allocating
// network resources for swarmkit resources. Give it objects, and it gives you
// resources.
//
// In order to keep concerns well-separated, the network allocator doesn't have
// direct access to the raft store. The caller will provide all of the objects,
// and be in charge of committing those objects to the raft store. The network
// allocator works directly on the api objects, mutates them, and gives them
// back to the caller to figure out how to store. This frees us from having to
// worry about the ultimate storage method, and reduces the already rather
// bloated surface area of this component.
//
// The network allocator provides the interface for the top-level API objects,
// and lower level component allocators operate on specific fields.
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
// See github.com/docker/swarmkit/api for documentation on what each object
// type is for.
//
// The allocator's methods always either fully allocate the object and return
// nil, or return an error and leave the object unaltered. The allocator will
// never leave objects in a partially-allocated state.
//
// # Local state
//
// The network allocator, and its subcomponents, rely heavily on internal,
// local state. All of the libnetwork libraries that the network allocator
// depends on have their own local state. This has the advantage of not needing
// to keep two different distributed states in sync. There is one sole source
// of truth about the network state in the cluster. However, if the local state
// of the allocator is not completely consistent with the distributed state,
// then errors such as double allocations or use-after-frees may result.
//
// In order to correctly allocate, the allocator provides a Restore method, to
// which all allocated and unallocated objects can be passed. It should be
// called only once, when the allocator is initialized. The Restore method runs
// through the state of all objects (and calls Restore methods on the
// subcomponents) to fully initialize the local state of the allocator. If the
// Restore method returns no errors, then the local state has been fully,
// correctly and consistently restored, and new allocation can proceed
// correctly. The Restore method should only be called once; subsequent calls
// have undefined behavior.
//
// # Subcomponents
//
// The real work of the allocator is done in the subcomponents, the driver,
// ipam, and port packages. These components are each concerned with specific
// parts of the network objects, and own the corresponding fields. The specific
// documentation for those components is located within their packages, but at
// a high level, they are described below
//
// ## `driver`
//
// The `driver` package is concerned with allocating network driver state for
// networks.  It only operates on Network objects, and totally owns their
// Driver field.
//
// ## `ipam`
//
// The `ipam` package is concerned with allocating IP addresses and IPAM driver
// state. It is the most complex package, and contains the most code. It
// provides the translation between the underlying libnetwork ipam libraries,
// which operates in terms of addresses and pools, and the swarmkit objects
// that require those things. The ipam package fully owns any NetworkAttachment
// fields, and fully owns the Endpoint's Virtual IPs. It also fully owns the
// IPAMConfig object for networks.
//
// ## `port`
//
// The port allocator, uniquely, does not rely on libnetwork. Its
// responsibility is to choose port numbers from the available port range, keep
// track of which ports are in use, and return errors to prevent overlapping
// port allocations. The PortAllocator is *not* concurrency safe, and as a
// result of its two-phase model, if the rest of the allocator is made
// concurrency safe, access to the port allocator will need to be protected by
// a lock outside of the port allocator itself.
