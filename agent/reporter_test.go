package agent

import (
	"errors"
	"fmt"
	"io"
	"math/rand"
	"sync"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func TestReporter(t *testing.T) {
	const ntasks = 100
	ctx := context.Background()
	stub := &stubReporter{t: t,
		statuses: make(map[string]*api.TaskStatus),
		unique:   make(map[uniqueStatus]struct{})}
	reporter := newStatusReporter(ctx, stub)

	expected := make(map[string]*api.TaskStatus)

	stub.wg.Add(ntasks) // statuses will be reported!

	for _, state := range []api.TaskState{
		api.TaskStateAccepted,
		api.TaskStatePreparing,
		api.TaskStateReady,
		api.TaskStateCompleted,
		api.TaskStateDead,
	} {
		for i := 0; i < ntasks; i++ {
			taskID, status := fmt.Sprint(i), &api.TaskStatus{State: state}
			expected[taskID] = status

			// simulate poinding this with a bunch of goroutines
			go func() {
				if err := reporter.UpdateTaskStatus(ctx, taskID, status); err != nil {
					t.Fatalf("sending should not fail: %v", err)
				}
			}()

		}
	}

	stub.wg.Wait() // wait for the propagation
	reporter.(io.Closer).Close()
	stub.mu.Lock()
	defer stub.mu.Unlock()

	assert.Equal(t, expected, stub.statuses)
}

type uniqueStatus struct {
	taskID string
	status *api.TaskStatus
}

type stubReporter struct {
	t        *testing.T
	wg       sync.WaitGroup
	statuses map[string]*api.TaskStatus
	unique   map[uniqueStatus]struct{} // track incoming by identity.
	mu       sync.Mutex
}

func (sr *stubReporter) UpdateTaskStatus(ctx context.Context, taskID string, status *api.TaskStatus) error {
	if rand.Float64() > 0.9 {
		return errors.New("status send failed")
	}

	sr.mu.Lock()
	defer sr.mu.Unlock()

	key := uniqueStatus{taskID, status}
	// make sure we get the status only once.
	if _, ok := sr.unique[key]; ok {
		sr.t.Fatalf("encountered status twice")
	}

	if status.State == api.TaskStateDead {
		sr.wg.Done()
	}

	sr.unique[key] = struct{}{}
	if current, ok := sr.statuses[taskID]; ok {
		if status.State <= current.State {
			return nil // only allow forward updates
		}
	}

	sr.statuses[taskID] = status

	return nil
}
