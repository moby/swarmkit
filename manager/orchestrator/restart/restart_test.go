package restart

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/state/store"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// longTestDelay keeps a registered delay parked in its timer for the whole
// test, so assertions about the task state are not affected by the delay's
// goroutine starting the task.
const longTestDelay = time.Hour

// watchdogTimeout is only a failure boundary for tests that must not hang;
// correctness never depends on it.
const watchdogTimeout = 30 * time.Second

func newTestTask(id string) *api.Task {
	return &api.Task{
		ID:           id,
		ServiceID:    "service1",
		Slot:         1,
		DesiredState: api.TaskStateReady,
	}
}

// runLockedBatch runs cb inside a store Batch, in a separate goroutine, and
// fails the test if the batch does not complete within the watchdog timeout.
// While cb runs, the batch holds the store's write lock, mirroring the
// in-transaction callers of DelayStart.
func runLockedBatch(t *testing.T, s *store.MemoryStore, cb func(*store.Batch) error) {
	t.Helper()
	batchDone := make(chan error, 1)
	go func() {
		batchDone <- s.Batch(cb)
	}()
	select {
	case err := <-batchDone:
		require.NoError(t, err)
	case <-time.After(watchdogTimeout):
		t.Fatal("Batch did not complete: DelayStart deadlocked while the store write lock was held")
	}
}

// waitForChannel fails the test if doneCh does not close within the watchdog
// timeout.
func waitForChannel(t *testing.T, doneCh <-chan struct{}, desc string) {
	t.Helper()
	select {
	case <-doneCh:
	case <-time.After(watchdogTimeout):
		t.Fatalf("%s did not terminate in time", desc)
	}
}

// cancelDelay cancels taskID's delay in a goroutine and fails the test if
// Cancel does not complete within the watchdog timeout.
func cancelDelay(t *testing.T, sup *Supervisor, taskID string) {
	t.Helper()
	cancelDone := make(chan struct{})
	go func() {
		sup.Cancel(taskID)
		close(cancelDone)
	}()
	select {
	case <-cancelDone:
	case <-time.After(watchdogTimeout):
		t.Fatal("Cancel did not complete")
	}
}

func taskDesiredState(t *testing.T, s *store.MemoryStore, taskID string) api.TaskState {
	t.Helper()
	var state api.TaskState
	s.View(func(tx store.ReadTx) {
		task := store.GetTask(tx, taskID)
		require.NotNil(t, task)
		state = task.DesiredState
	})
	return state
}

// Regression test for the deadlock in
// https://github.com/moby/moby/issues/54128: DelayStart used to wait for a
// superseded delay's doneCh while the caller held the store's write lock. A
// superseded goroutine that was blocked acquiring that lock could therefore
// never finish, deadlocking the caller's Batch and, with it, every
// subsequent store write.
func TestDelayStartReplacementDoesNotDeadlock(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	defer s.Close()
	sup := NewSupervisor(s)

	require.NoError(t, s.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, newTestTask("task1"))
	}))

	var ch1 <-chan struct{}
	runLockedBatch(t, s, func(batch *store.Batch) error {
		// delay 0 and waitStop false: the registered goroutine has no
		// select to wait in, so while this callback holds the write
		// lock it deterministically ends up blocked in store.Update.
		ch1 = sup.DelayStart(ctx, nil, nil, "task1", 0, false)
		// Replace it for the same task while still inside the
		// transaction. Before the fix, this waited for ch1 to close,
		// which can never happen here.
		sup.DelayStart(ctx, nil, nil, "task1", longTestDelay, false)
		return nil
	})

	// The superseded goroutine terminates...
	waitForChannel(t, ch1, "superseded delay goroutine")
	// ...without starting the task. The replacement delay is parked in
	// its timer, so the task state is deterministic.
	assert.Equal(t, api.TaskStateReady, taskDesiredState(t, s, "task1"))

	// The replacement delay remains registered after the superseded
	// goroutine's cleanup (identity-guarded deletion).
	sup.mu.Lock()
	registered := sup.delays["task1"]
	sup.mu.Unlock()
	require.NotNil(t, registered, "replacement delay entry missing after superseded goroutine exited")

	// The replacement delay can be cancelled.
	cancelDelay(t, sup, "task1")
	assert.Equal(t, api.TaskStateReady, taskDesiredState(t, s, "task1"))

	// After the cancelled goroutine's cleanup, no entry remains.
	sup.mu.Lock()
	assert.Empty(t, sup.delays)
	sup.mu.Unlock()
}

// The superseded goroutine's deferred cleanup must not delete the newer
// delay's map entry.
func TestDelayStartSupersededCleanupKeepsNewerDelay(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	defer s.Close()
	sup := NewSupervisor(s)

	require.NoError(t, s.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, newTestTask("task1"))
	}))

	var ch1, ch2 <-chan struct{}
	runLockedBatch(t, s, func(batch *store.Batch) error {
		ch1 = sup.DelayStart(ctx, nil, nil, "task1", 0, false)
		ch2 = sup.DelayStart(ctx, nil, nil, "task1", longTestDelay, false)
		return nil
	})

	waitForChannel(t, ch1, "superseded delay goroutine")

	// The surviving entry must still be the newer delay's.
	sup.mu.Lock()
	current := sup.delays["task1"]
	sup.mu.Unlock()
	require.NotNil(t, current, "newer delay entry was deleted by the superseded goroutine")
	assert.True(t, current.doneCh == ch2, "surviving map entry does not belong to the newer delay")

	// Cancelling the registered delay closes the newer delay's channel:
	// behavioral proof that the entry in the map belongs to ch2.
	cancelDelay(t, sup, "task1")
	waitForChannel(t, ch2, "replacement delay goroutine")
	assert.Equal(t, api.TaskStateReady, taskDesiredState(t, s, "task1"))

	sup.mu.Lock()
	assert.Empty(t, sup.delays)
	sup.mu.Unlock()
}

// Rapid repeated replacements must not orphan or delete the latest delay,
// and every superseded goroutine must exit.
func TestDelayStartRepeatedReplacement(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	defer s.Close()
	sup := NewSupervisor(s)

	require.NoError(t, s.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, newTestTask("task1"))
	}))

	const replacements = 5
	chans := make([]<-chan struct{}, replacements)
	runLockedBatch(t, s, func(batch *store.Batch) error {
		// All but the last delay are superseded synchronously by the
		// next call; the last one stays live and parked in its timer.
		for i := 0; i < replacements; i++ {
			var delay time.Duration
			if i == replacements-1 {
				delay = longTestDelay
			}
			chans[i] = sup.DelayStart(ctx, nil, nil, "task1", delay, false)
		}
		return nil
	})

	// Every superseded goroutine must terminate without starting the
	// task; the live one stays parked in its timer until it is
	// cancelled below.
	for i := 0; i < replacements-1; i++ {
		waitForChannel(t, chans[i], fmt.Sprintf("superseded delay goroutine %d", i))
	}
	assert.Equal(t, api.TaskStateReady, taskDesiredState(t, s, "task1"))

	// The latest delay must still be the registered one.
	sup.mu.Lock()
	current := sup.delays["task1"]
	sup.mu.Unlock()
	require.NotNil(t, current, "latest delay entry was deleted by a superseded goroutine")
	assert.True(t, current.doneCh == chans[replacements-1], "surviving map entry does not belong to the latest delay")

	cancelDelay(t, sup, "task1")
	waitForChannel(t, chans[replacements-1], "live delay goroutine")
	sup.mu.Lock()
	assert.Empty(t, sup.delays)
	sup.mu.Unlock()
}

// A live (non-superseded) delayed start still moves the task out of READY
// once the batch releases the write lock.
func TestDelayStartStartsTask(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemoryStore(nil)
	defer s.Close()
	sup := NewSupervisor(s)

	require.NoError(t, s.Update(func(tx store.Tx) error {
		return store.CreateTask(tx, newTestTask("task1"))
	}))

	runLockedBatch(t, s, func(batch *store.Batch) error {
		sup.DelayStart(ctx, nil, nil, "task1", 0, false)
		return nil
	})

	require.Eventually(t, func() bool {
		return taskDesiredState(t, s, "task1") == api.TaskStateRunning
	}, watchdogTimeout, 10*time.Millisecond)

	sup.mu.Lock()
	assert.Empty(t, sup.delays)
	sup.mu.Unlock()
}
