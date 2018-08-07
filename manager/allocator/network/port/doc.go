package port

// The `port` package contains the implementation of the swarmkit Allocator for
// network ports. Its purpose is to assign ports to Endpoint objects based on
// the provided Spec. For ingress ports, ports which are exposed through the
// ingress network on every node in the cluster, it keeps track of the
// availability of ports, and handles dynamic allocation of ports when the user
// has specified no PublishedPort. The allocator does _not_ keep track of
// host-mode ports, those which are only exposed locally on the node, in any
// capacity. It adds them to the returned Ports assignment, without checking
// their validity or availability. Port assignments, validity, and
// availability, for host-mode ports are handled solely on the worker.
//
// Notably, the allocator only keeps track of which ports are in use, not which
// services they are in use by. For the port allocator to function correctly,
// the objects it works with must be in a consistent state. To facilitate this,
// the port allocator "owns" the Endpoint.Ports field, and should be the only
// component that creates the PortConfig objects contained in it.
//
// Before the Allocator can be used, it needs to be initialized by calling its
// Restore method. This populates the state of the Allocator with all of the
// endpoints in use.
//
// The Allocator follows a two-phase transaction-style lifecycle. Calls to
// Allocate or Deallocate return a `Proposal` object, which contains the
// necessary information to commit the change. Calling the `Commit` method on
// the Proposal will commit the changes to the Allocator. If the caller decides
// to abandon the changes, the user can simply abandon the Proposal without
// calling Commit.
//
// In the interest of simplicity, the PortAllocator is *not* concurrency-safe.
// Access to the PortAllocator must be made serially, and each operation's
// changes must be committed before the next operation can correctly proceed.
// If another call to Allocate is made before the previous allocation is
// Committed, the two allocations may conflict.
