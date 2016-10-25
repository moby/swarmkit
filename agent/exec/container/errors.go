package container

import "errors"

var (
	// ErrContainerDestroyed returned when a container is prematurely destroyed
	// during a wait call.
	ErrContainerDestroyed = errors.New("dockerexec: container destroyed")

	// ErrContainerUnhealthy returned if controller detects the health check failure
	ErrContainerUnhealthy = errors.New("dockerexec: unhealthy container")
)
