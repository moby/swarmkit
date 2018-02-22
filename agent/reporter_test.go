package agent

import (
	"fmt"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

type uniqueStatus struct {
	taskID string
	status *api.TaskStatus
}

func TestReporter(t *testing.T) {
	const ntasks = 100

	var (
		// NOTE(dperny): don't increase this timeout! this test should never
		// take anything close to a minute. If it does, you have a bug
		// someplace else. Probably a deadlock.
		ctx, cancel = context.WithTimeout(context.Background(), 60*time.Second)
		statuses    = make(map[string]*api.TaskStatus) // destination map
		unique      = make(map[uniqueStatus]struct{})  // ensure we don't receive any status twice
		mu          sync.Mutex
		expected    = make(map[string]*api.TaskStatus)
		// in the past, this test used a waitgroup to determine when all
		// statuses had been reported. the problem is that if the test fails
		// and some statuses don't get reported, the test would deadlock.
		// instead, we use a channel here, so we can select on that channel and
		// ctx.Done(), allowing us to gracefully exit the test on timeout.
		remaining = ntasks
		done      = make(chan struct{})
	)
	defer cancel()

	reporter := newStatusReporter(ctx, statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		if rand.Float64() > 0.9 {
			return fmt.Errorf("status send failed for %v to %v", taskID, status.State)
		}

		mu.Lock()
		defer mu.Unlock()

		key := uniqueStatus{taskID, status}
		// make sure we get the status only once.
		if _, ok := unique[key]; ok {
			// do not Fatal here. This runs in a goroutine and Fatal won't
			// cause the test to exit.
			t.Errorf("got update for %v to %v twice", taskID, status.State)
			// return here, don't release a wg. also don't return error, which
			// will cause the batch to be redone indefinitely.
			// TODO(dperny): duplicate updates isn't an error condition
			return nil
		}

		if status.State == api.TaskStateCompleted {
			remaining = remaining - 1
			if remaining <= 0 {
				// defer the close so it runs after we finish this run
				defer close(done)
			}
		}

		unique[key] = struct{}{}
		if current, ok := statuses[taskID]; ok {
			if status.State <= current.State {
				return nil // only allow forward updates
			}
		}

		statuses[taskID] = status

		return nil
	}))

	for _, state := range []api.TaskState{
		api.TaskStateAccepted,
		api.TaskStatePreparing,
		api.TaskStateReady,
		api.TaskStateCompleted,
	} {
		for i := 0; i < ntasks; i++ {
			taskID, status := fmt.Sprint(i), &api.TaskStatus{State: state}
			expected[taskID] = status

			// simulate pounding this with a bunch of goroutines
			go func() {
				if err := reporter.UpdateTaskStatus(ctx, map[string]*api.TaskStatus{taskID: status}); err != nil {
					assert.NoError(t, err, "sending should not fail")
				}
			}()

		}
	}

	select {
	case <-ctx.Done():
		t.Error("context done. you probably have a deadlock somewhere")
	case <-done:
		// we're done, finish out the test.
	}
	assert.NoError(t, reporter.Close())
	mu.Lock()
	defer mu.Unlock()

	assert.Equal(t, expected, statuses)
}
