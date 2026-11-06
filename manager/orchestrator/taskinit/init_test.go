package taskinit

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/moby/swarmkit/v2/manager/state/testutils"
	"github.com/stretchr/testify/require"
)

// sizeLimitedProposer enforces the serialized request limit used by Raft.
type sizeLimitedProposer struct {
	testutils.MockProposer
}

func (p *sizeLimitedProposer) ProposeValue(ctx context.Context, actions []api.StoreAction, cb func()) error {
	request := api.InternalRaftRequest{Action: actions}
	if size := request.Size(); size > store.MaxTransactionBytes {
		return fmt.Errorf("raft request size %d exceeds limit %d", size, int(store.MaxTransactionBytes))
	}
	return p.MockProposer.ProposeValue(ctx, actions, cb)
}

// unrelatedInitHandler leaves tasks belonging to another orchestrator alone.
type unrelatedInitHandler struct{}

func (unrelatedInitHandler) IsRelatedService(*api.Service) bool               { return false }
func (unrelatedInitHandler) FixTask(context.Context, *store.Batch, *api.Task) {}
func (unrelatedInitHandler) SlotTuple(*api.Task) orchestrator.SlotTuple {
	return orchestrator.SlotTuple{}
}

func TestCheckTasksLargeOrphanTasks(t *testing.T) {
	s := store.NewMemoryStore(&sizeLimitedProposer{})
	defer s.Close()

	// Each task can be created and deleted independently, but two deletion
	// actions together exceed Raft's limit before Batch checks its threshold.
	for _, id := range []string{"orphan-1", "orphan-2", "orphan-3"} {
		task := &api.Task{
			ID:        id,
			ServiceID: "deleted-service",
			Spec: api.TaskSpec{Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Image: "busybox:latest",
					Env:   []string{"PAYLOAD=" + strings.Repeat("x", 850*1024)},
				},
			}},
			Status: api.TaskStatus{State: api.TaskStateFailed},
		}
		require.NoError(t, s.Update(func(tx store.Tx) error {
			return store.CreateTask(tx, task)
		}))
	}
	require.NoError(t, s.Update(func(tx store.Tx) error {
		if err := store.CreateService(tx, &api.Service{ID: "existing-service"}); err != nil {
			return err
		}
		if err := store.CreateTask(tx, &api.Task{ID: "retained", ServiceID: "existing-service"}); err != nil {
			return err
		}
		return store.CreateTask(tx, &api.Task{ID: "standalone"})
	}))

	s.View(func(readTx store.ReadTx) {
		require.NoError(t, CheckTasks(context.Background(), s, readTx, unrelatedInitHandler{}, nil))
	})
	s.View(func(tx store.ReadTx) {
		for _, id := range []string{"orphan-1", "orphan-2", "orphan-3"} {
			require.Nil(t, store.GetTask(tx, id))
		}
		require.NotNil(t, store.GetTask(tx, "retained"))
		require.NotNil(t, store.GetTask(tx, "standalone"))
	})
}
