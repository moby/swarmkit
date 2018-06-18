package helpers

import (
	"github.com/docker/swarmkit/api"
	"runtime"
)

const (
	// PredefinedLabel identifies internally allocated swarm networks
	// corresponding to the node-local predefined networks on the host.
	PredefinedLabel = "com.docker.swarm.predefined"
)

// IsBuiltInNetworkDriver returns true if the provided driver is one of the
// built-in network drivers. It is used primarily to validate specs in the
// controlapi package.
func IsBuiltInNetworkDriver(driver string) bool {
	// TODO(dperny): there's probably a better way to do this, but it's going
	// to be a real pain of rearchitecting... basically, this is a copy of the
	// built-in drivers which are found in the drivers_platform.go files in the
	// allocator/network package. If those change, this needs to change too.
	// It's easier to do it this way than to properly rearchitect the
	// platform-specific driver initialization code.
	switch runtime.GOOS {
	case `linux`:
		switch driver {
		case "remote", "overlay", "macvlan", "bridge", "ipvlan", "host":
			return true
		}
	case `darwin`, `windows`:
		switch driver {
		case "remote", "overlay":
			return true
		}
	}

	return false
}

// IsIngress returns true if the provided network is an ingress network
//
// A network is an ingress network if the Ingress field on the spec is set to
// true, or, for backward compatibility, if the "com.docker.swarm.internal"
// label is set and the name of the network is "ingress".
func IsIngress(nw *api.Network) bool {
	// nil networks are never ingress
	if nw == nil {
		return false
	}

	if nw.Spec.Ingress {
		return true
	}

	// older networks, which predate the existence of the "Ingress" field on
	// the spec, were marked as ingress networks this way.
	_, ok := nw.Spec.Annotations.Labels["com.docker.swarm.internal"]
	if ok && nw.Spec.Annotations.Name == "ingress" {
		return true
	}

	return false
}

// IsIngressNetworkNeeded checks whether the given endpoint spec requires the
// routing mesh.
func IsIngressNetworkNeeded(endpoint *api.EndpointSpec) bool {
	if endpoint == nil {
		return false
	}

	for _, p := range endpoint.Ports {
		// The service to which this task belongs is trying to
		// expose ports with PublishMode as Ingress to the
		// external world. Automatically attach the task to
		// the ingress network.
		if p.PublishMode == api.PublishModeIngress {
			return true
		}
	}

	return false
}
