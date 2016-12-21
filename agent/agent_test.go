package agent

import (
	"testing"
	"time"

	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/ca"
	"github.com/docker/swarmkit/ca/testutils"
	"github.com/docker/swarmkit/connectionbroker"
	"github.com/docker/swarmkit/remotes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	// 	is currently exposed through remotes.
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
	tc := testutils.NewTestCA(t)
	defer tc.Stop()

	agentSecurityConfig, err := tc.NewNodeConfig(ca.WorkerRole)
	require.NoError(t, err)

	addr := "localhost:4949"
	remotes := remotes.NewRemotes(api.Peer{Addr: addr})

	db, cleanup := storageTestEnv(t)
	defer cleanup()

	agent, err := New(&Config{
		Executor:    &NoopExecutor{},
		ConnBroker:  connectionbroker.New(remotes),
		Credentials: agentSecurityConfig.ClientTLSCreds,
		DB:          db,
	})
	require.NoError(t, err)
	assert.NotNil(t, agent)

	ctx, _ := context.WithTimeout(context.Background(), 5000*time.Millisecond)

	assert.Equal(t, errAgentNotStarted, agent.Stop(ctx))
	assert.NoError(t, agent.Start(ctx))

	if err := agent.Start(ctx); err != errAgentStarted {
		t.Fatalf("expected agent started error: %v", err)
	}

	assert.NoError(t, agent.Stop(ctx))
}

func TestHandleSessionMessage(t *testing.T) {
	// TODO(rajdeepd): The current agent is fairly monolithic, hence we
	// have to test message handling this way. Needs to be refactored
	ctx, _ := context.WithTimeout(context.Background(), 5000*time.Millisecond)
	agent, cleanup := agentTestEnv(t)

	defer cleanup()

	var messages = []*api.SessionMessage{
		{SessionID: "sm1", Node: &api.Node{},
			Managers: []*api.WeightedPeer{
				{&api.Peer{NodeID: "node1", Addr: "10.0.0.1"}, 1.0}},
			NetworkBootstrapKeys: []*api.EncryptionKey{{}}},
		{SessionID: "sm1", Node: &api.Node{},
			Managers: []*api.WeightedPeer{
				{&api.Peer{NodeID: "node1", Addr: ""}, 1.0}},
			NetworkBootstrapKeys: []*api.EncryptionKey{{}}},
		{SessionID: "sm1", Node: &api.Node{},
			Managers: []*api.WeightedPeer{
				{&api.Peer{NodeID: "node1", Addr: "10.0.0.1"}, 1.0}},
			NetworkBootstrapKeys: nil},
		{SessionID: "sm1", Node: &api.Node{},
			Managers: []*api.WeightedPeer{
				{&api.Peer{NodeID: "", Addr: "10.0.0.1"}, 1.0}},
			NetworkBootstrapKeys: []*api.EncryptionKey{{}}},
		{SessionID: "sm1", Node: &api.Node{},
			Managers: []*api.WeightedPeer{
				{&api.Peer{NodeID: "node1", Addr: "10.0.0.1"}, 0.0}},
			NetworkBootstrapKeys: []*api.EncryptionKey{{}}},
	}

	for _, m := range messages {
		err := agent.handleSessionMessage(ctx, m)
		if err != nil {
			t.Fatalf("err should be nil")
		}
	}
}

func agentTestEnv(t *testing.T) (*Agent, func()) {
	var cleanup []func()
	tc := testutils.NewTestCA(t)
	cleanup = append(cleanup, func() { tc.Stop() })

	agentSecurityConfig, err := tc.NewNodeConfig(ca.WorkerRole)
	require.NoError(t, err)

	addr := "localhost:4949"
	remotes := remotes.NewRemotes(api.Peer{Addr: addr})

	db, cleanupStorage := storageTestEnv(t)
	cleanup = append(cleanup, func() { cleanupStorage() })

	agent, err := New(&Config{
		Executor:    &NoopExecutor{},
		ConnBroker:  connectionbroker.New(remotes),
		Credentials: agentSecurityConfig.ClientTLSCreds,
		DB:          db,
	})
	require.NoError(t, err)
	return agent, func() {
		for i := len(cleanup) - 1; i >= 0; i-- {
			cleanup[i]()
		}
	}
}
