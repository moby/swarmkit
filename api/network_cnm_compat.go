package api

// Convenience functions for dealing with NetworkSpec and Network
// objects with compat CNM fields.

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
		return t.CNM.Copy()
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

// GetCNMCompat is like GetCNM but if necessary falls back to legacy
// fields in the Network object. Returns `nil` if the Network object
// is not a CNM object.
//
// Callers which modify the returned state object and wish for those
// modifications to persist _must_ call SetCNMCompat() after making
// their changes. (perhaps by calling "defer n.SetCNMCompat(state)"?)
//
// Callers should not hold the returned CNMState over calls to the
// allocator if they care about freshness, they should call
// GetCNMCompat again to obtain a fresh copy.
func (n *Network) GetCNMCompat() *CNMState {
	// The location of the state (compat or not) always
	// corresponds to the compat status of the spec. Hence we
	// check n.Spec not n.State
	switch n.Spec.Backend.(type) {
	case *NetworkSpec_CNM:
		if n.State == nil { // State not yet set?
			return &CNMState{}
		}
		state, ok := n.State.(*Network_CNM)
		if !ok {
			panic("Unexpected non CNM state on CNM Network")
		}
		// This is a bit more expensive than simply returning
		// state but avoids callers accidentally relying on
		// being able to write this struct directly.
		return state.CNM.Copy()
	case nil:
		return &CNMState{
			DriverState: n.CNMCompatDriverState,
			IPAM:        n.CNMCompatIPAM,
		}
	default:
		return nil
	}
}

// SetCNMCompat updates the CNMState of the Network. Callers who
// modify the result of GetCNMCompat must call this afterwards.
func (n *Network) SetCNMCompat(state *CNMState) {
	// The location of the state (compat or not) always
	// corresponds to the compat status of the spec. Hence we
	// check n.Spec not n.State
	switch n.Spec.Backend.(type) {
	case *NetworkSpec_CNM:
		n.State = &Network_CNM{
			CNM: state,
		}
	case nil:
		n.CNMCompatDriverState = state.DriverState
		n.CNMCompatIPAM = state.IPAM
	default:
		panic("Attempt to set CNM State on a non-CNM network object")
	}
}
