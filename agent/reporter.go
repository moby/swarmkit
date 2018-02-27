package agent

import (
	"reflect"
	"sync"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"golang.org/x/net/context"
)

// StatusReporter receives updates to task status. Method may be called
// concurrently, so implementations should be goroutine-safe.
type StatusReporter interface {
	UpdateTaskStatus(ctx context.Context, statuses map[string]*api.TaskStatus) error
}

type statusReporterFunc func(ctx context.Context, taskID string, status *api.TaskStatus) error

func (fn statusReporterFunc) UpdateTaskStatus(ctx context.Context, statuses map[string]*api.TaskStatus) error {
	for taskID, status := range statuses {
		if err := fn(ctx, taskID, status); err != nil {
			return err
		}
		// delete statuses from the map as we process them so the random failures
		// can't sink every batch forever
		delete(statuses, taskID)
	}
	return nil
}

// statusReporter creates a reliable StatusReporter that will always succeed.
// It handles several tasks at once, ensuring all statuses are reported.
//
// The reporter will continue reporting the current status until it succeeds.
type statusReporter struct {
	reporter StatusReporter
	statuses map[string]*api.TaskStatus
	mu       sync.Mutex
	cond     sync.Cond
	closed   bool
}

func newStatusReporter(ctx context.Context, upstream StatusReporter) *statusReporter {
	r := &statusReporter{
		reporter: upstream,
		statuses: make(map[string]*api.TaskStatus),
	}

	r.cond.L = &r.mu

	go r.run(ctx)
	return r
}

// UpdateTaskStatus updates the provided task statuses
func (sr *statusReporter) UpdateTaskStatus(ctx context.Context, statuses map[string]*api.TaskStatus) error {
	ctx = log.WithField(ctx, "method", "(*statusReporter).UpdateTaskStatus")
	log.G(ctx).Debugf("Updating %v task statuses", len(statuses))
	sr.mu.Lock()
	defer sr.mu.Unlock()
	for taskID, status := range statuses {
		sr.addStatus(taskID, status)
	}

	// the useful thing about this function is this: it doesn't wake the
	// waiting loop in run until everything has been added.
	sr.cond.Signal()

	return nil
}

// addStatus adds the passed status to the statuses map if it's the newest
// update
func (sr *statusReporter) addStatus(taskID string, status *api.TaskStatus) {
	current, ok := sr.statuses[taskID]
	if ok {
		if reflect.DeepEqual(current, status) {
			return
		}

		if current.State > status.State {
			return // ignore old updates
		}
	}
	sr.statuses[taskID] = status
}

func (sr *statusReporter) Close() error {
	sr.mu.Lock()
	defer sr.mu.Unlock()

	sr.closed = true
	sr.cond.Signal()

	return nil
}

func (sr *statusReporter) run(ctx context.Context) {
	ctx = log.WithModule(ctx, "reporter")
	done := make(chan struct{})
	defer close(done)

	sr.mu.Lock() // released during wait, below.
	defer sr.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			sr.Close()
		case <-done:
			return
		}
	}()

	for {
		if len(sr.statuses) == 0 {
			sr.cond.Wait()
		}

		if sr.closed {
			// TODO(stevvooe): Add support here for waiting until all
			// statuses are flushed before shutting down.
			return
		}

		// swap out the statuses map, so we can release the lock while we
		// do the batch
		statuses := sr.statuses
		sr.statuses = map[string]*api.TaskStatus{}
		// unlock the map so new statuses can be added while we process the
		// current ones
		sr.mu.Unlock()
		err := sr.reporter.UpdateTaskStatus(ctx, statuses)
		// re-lock the map so we can add statuses back if we need to.
		sr.mu.Lock()
		// it's possible that the status reporter may have closed while the
		// lock was released, but as the code is structured now, that's
		// unimportant. we might add some statuses back to the map, which would
		// be useless, but when we back get around to the sr.closed check
		// above, we'll exit anyway.
		if err != nil {
			// this is probably just a hiccup in the dispatcher, not an
			// unrecoverable error
			log.G(ctx).WithError(err).Warn("status reporter failed to batch-update tasks")
			// if the batch failed, we don't know what statuses worked or
			// not. stick them all back into the statuses map and process them
			// one by one
			for id, status := range statuses {
				sr.addStatus(id, status)
			}
		}
	}
}
