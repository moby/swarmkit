package ipam

// ipam is a package for interfacing with the libnetwork IPAM drivers.
//
// IPAM stands for "IP Address Management"
//
// libnetwork provides an interface for IPAM drivers. This interface does not
// operate on swarmkit objects. The purpose of the ipam package is to bridge
// the gap between swarmkit objects and the primitives that libnetwork uses.
//
// Swarmkit is the sole source of truth for the state of IPAM in in the
// swarmkit cluster; the IPAM drivers should not carry their own distributed
// state. The benefit to this is that the state of the IPAM drivers cannot
// become out of sync with the state of swarmkit. However, the drawback is that
// the IPAM drivers still rely on their own local state, which must be
// bootstrapped before IP address allocation can proceed correctly.
// Bootstrapping, in this case, means that when a fresh allocator comes up, we
// must first populate it with all of the addresses we know to be in use before
// allocating new ones.
//
// When initializing the IPAM drivers, they may take a data store, which allows
// them to persist state. We pass no data store, and therefore the IPAM drivers
// should not persist state beyond what is currently in memory. Further, the
// in-memory state should be fresh every time an allocator starts, even if the
// node is still running from the last time.
//
// IPAM operates in terms of pools an addresses. A pool is a set of IP
// addresses that attachments to a network can use. It is specified in terms of
// a subnet. An address is one individual IP address that belongs to a pool.
// Each pool has an ID associated with it that is used to reference the pool
// when allocating and deallocating addresses. This pool ID is not persistent,
// and changes every time the allocator is reinitialized.
//
// The ipam package has sole authority over the following fields, and should be
// the only component that writes to them.
//
// - api.Network.IPAM
// - api.Task.Attachments
// - api.Service.Endpoint.VirtualIPs
