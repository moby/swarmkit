package manager

import (
	"context"
	"os"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/ca"
	"github.com/moby/swarmkit/v2/ca/testutils"
	"github.com/moby/swarmkit/v2/manager/state"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsStateDirty(t *testing.T) {
	ctx := context.Background()

	temp, err := os.CreateTemp("", "test-socket")
	require.NoError(t, err)
	require.NoError(t, temp.Close())
	require.NoError(t, os.Remove(temp.Name()))

	defer os.RemoveAll(temp.Name())

	tc := testutils.NewTestCA(t, func(p ca.CertPaths) *ca.KeyReadWriter {
		return ca.NewKeyReadWriter(p, []byte("kek"), nil)
	})
	defer tc.Stop()

	managerSecurityConfig, err := tc.NewNodeConfig(ca.ManagerRole)
	require.NoError(t, err)

	stateDir := t.TempDir()
	m, err := New(&Config{
		RemoteAPI:        &RemoteAddrs{ListenAddr: "127.0.0.1:0"},
		ControlAPI:       temp.Name(),
		StateDir:         stateDir,
		SecurityConfig:   managerSecurityConfig,
		AutoLockManagers: true,
		UnlockKey:        []byte("kek"),
		RootCAPaths:      tc.Paths.RootCA,
	})
	require.NoError(t, err)
	assert.NotNil(t, m)

	go m.Run(ctx)
	defer m.Stop(ctx, false)

	// State should never be dirty just after creating the manager
	isDirty, err := m.IsStateDirty()
	require.NoError(t, err)
	assert.False(t, isDirty)

	// Wait for cluster and node to be created.
	watch, cancel := state.Watch(m.raftNode.MemoryStore().WatchQueue())
	defer cancel()
	<-watch
	<-watch

	// Updating the node should not cause the state to become dirty
	require.NoError(t, m.raftNode.MemoryStore().Update(func(tx store.Tx) error {
		node := store.GetNode(tx, m.config.SecurityConfig.ClientTLSCreds.NodeID())
		require.NotNil(t, node)
		node.Spec.Availability = api.NodeAvailabilityPause
		return store.UpdateNode(tx, node)
	}))
	isDirty, err = m.IsStateDirty()
	require.NoError(t, err)
	assert.False(t, isDirty)

	// Adding a service should cause the state to become dirty
	require.NoError(t, m.raftNode.MemoryStore().Update(func(tx store.Tx) error {
		return store.CreateService(tx, &api.Service{ID: "foo"})
	}))
	isDirty, err = m.IsStateDirty()
	require.NoError(t, err)
	assert.True(t, isDirty)
}
