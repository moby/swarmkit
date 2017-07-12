package networkallocator

import (
	"github.com/docker/swarmkit/api"
)

const (
	// PredefinedLabel identifies internally allocated swarm networks
	// corresponding to the node-local predefined networks on the host.
	PredefinedLabel = "com.docker.swarm.predefined"
)

// IsIngressNetwork check if the network is an ingress network
func IsIngressNetwork(nw *api.Network) bool {
	if nw.Spec.Ingress {
		return true
	}
	// Check if legacy defined ingress network
	_, ok := nw.Spec.Annotations.Labels["com.docker.swarm.internal"]
	return ok && nw.Spec.Annotations.Name == "ingress"
}

// IsIngressNetworkNeeded checks whether the service requires the routing-mesh
func IsIngressNetworkNeeded(s *api.Service) bool {
	if s == nil {
		return false
	}

	if s.Spec.Endpoint == nil {
		return false
	}

	for _, p := range s.Spec.Endpoint.Ports {
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
