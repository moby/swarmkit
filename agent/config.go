package agent

import "fmt"

// Config provides values for an Agent.
type Config struct {
	// ID is the identifier to be used for the agent.
	ID string

	// Hostname the name of host for agent instance.
	Hostname string

	// Managers provides the manager backend used by the agent. It will be
	// updated with managers weights as observed by the agent.
	Managers Managers
}

func (c *Config) validate() error {
	if c.ID == "" {
		return fmt.Errorf("config: id required")
	}

	if c.Hostname == "" {
		return fmt.Errorf("config: name required")
	}

	return nil
}
