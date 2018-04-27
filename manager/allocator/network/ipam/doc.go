package ipam

// ipam is a package for interfacing with the libnetwork IPAM drivers.
// It performs the work of initializing and bootstrapping the IPAM state for
// networks, keeping track of IPAM pools, and allocating new addresses for VIPs
// and network attachments.
//
// The ipam package has sole authority over the following fields, and should be
// the only component that writes to them
//
// - api.Network.IPAM
// - api.Task.Attachments
// - api.Service.Endpoint.VirtualIPs
