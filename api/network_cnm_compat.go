package api

// Convenience functions for dealing with NetworkSpec and Network
// objects with compat CNM fields.

import (
	"github.com/docker/swarmkit/api/deepcopy"
)

// GetCNMCompat is like GetCNM but if necessary falls back to legacy
// fields in the NetworkSpec. Returns `nil` if the Network object is
// not a CNM object.
//
// Callers should not expect modifications to the returned structure
// to update the NetworkSpec and it is not expected that anything will
// ever need to write those fields after they are set by the
// originating client. Hence no corresponding SetCNMCompat method is
// provided.
func (ns *NetworkSpec) GetCNMCompat() *CNMNetworkSpec {
	switch t := ns.Backend.(type) {
	case *NetworkSpec_CNM:
		// This is a bit more expensive than simply returning
		// t.CNM but avoids callers accidentally relying on
		// being able to write this struct.
		var spec CNMNetworkSpec
		deepcopy.Copy(&spec, t.CNM)
		return &spec
	case nil:
		return &CNMNetworkSpec{
			DriverConfig: ns.CNMCompatDriverConfig,
			Ipv6Enabled:  ns.CNMCompatIpv6Enabled,
			Internal:     ns.CNMCompatInternal,
			IPAM:         ns.CNMCompatIPAM,
			Attachable:   ns.CNMCompatAttachable,
			Ingress:      ns.CNMCompatIngress,
		}
	default:
		return nil
	}
}
