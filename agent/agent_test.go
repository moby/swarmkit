package agent

import (
	"testing"
	"time"

	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/ca"
	"github.com/docker/swarm-v2/ca/testutils"
	"github.com/docker/swarm-v2/picker"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

// NoopExecutor is a dummy executor that implements enough to get the agent started.
type NoopExecutor struct {
}

func (e *NoopExecutor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	return &api.NodeDescription{}, nil
}

func (e *NoopExecutor) Configure(ctx context.Context, node *api.Node) error {
	return nil
}

func (e *NoopExecutor) Controller(t *api.Task) (exec.Controller, error) {
	return nil, exec.ErrRuntimeUnsupported
}

func (e *NoopExecutor) SetNetworkBootstrapKeys([]*api.EncryptionKey) error {
	return nil
}

func TestAgent(t *testing.T) {
	// TODO(stevvooe): The current agent is fairly monolithic, making it hard
	// to test without implementing or mocking an entire master. We'd like to
	// avoid this, as these kinds of tests are expensive to maintain.
	//
	// To support a proper testing program, the plan is to decouple the agent
	// into the following components:
	//
	// 	Connection: Manages the RPC connection and the available managers. Must
	// 	follow lazy grpc style but also expose primitives to force reset, which
	// 	is currently exposed through picker.
	//
	//	Session: Manages the lifecycle of an agent from Register to a failure.
	//	Currently, this is implemented as Agent.session but we'd prefer to
	//	encapsulate it to keep the agent simple.
	//
	// 	Agent: With the above scaffolding, the agent reduces to Agent.Assign
	// 	and Agent.Watch. Testing becomes as simple as assigning tasks sets and
	// 	checking that the appropriate events come up on the watch queue.
	//
	// We may also move the Assign/Watch to a Driver type and have the agent
	// oversee everything.
}

func TestAgentStartStop(t *testing.T) {
	tc := testutils.NewTestCA(t, testutils.AutoAcceptPolicy())
	defer tc.Stop()

	agentSecurityConfig, err := tc.NewNodeConfig(ca.AgentRole)
	assert.NoError(t, err)

	agent, err := New(&Config{
		Executor:       &NoopExecutor{},
		Managers:       picker.NewRemotes("localhost:4949"),
		SecurityConfig: agentSecurityConfig,
	})
	assert.NoError(t, err)
	assert.NotNil(t, agent)

	ctx, _ := context.WithTimeout(context.Background(), 5000*time.Millisecond)

	assert.Equal(t, errAgentNotStarted, agent.Stop(ctx))
	assert.NoError(t, agent.Start(ctx))

	if err := agent.Start(ctx); err != errAgentStarted {
		t.Fatalf("expected agent started error: %v", err)
	}

	assert.NoError(t, agent.Stop(ctx))
}
