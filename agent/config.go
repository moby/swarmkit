package agent

import (
	"fmt"

	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/picker"
)

// Config provides values for an Agent.
type Config struct {
	// Hostname the name of host for agent instance.
	Hostname string

	// Managers provides the manager backend used by the agent. It will be
	// updated with managers weights as observed by the agent.
	Managers picker.Remotes

	// Executor specifies the executor to use for the agent.
	Executor exec.Executor

	// SecurityConfig specifies the security configuration of the Agent
	SecurityConfig *ca.AgentSecurityConfig
}

func (c *Config) validate() error {
	if c.SecurityConfig == nil {
		return fmt.Errorf("config: SecurityConfig required")
	}

	return nil
}
