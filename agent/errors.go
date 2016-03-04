package agent

import "fmt"

var (
	errNodeNotRegistered   = fmt.Errorf("node not registered")
	errManagersUnavailable = fmt.Errorf("managers unavailable")
)
