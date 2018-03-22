package logger

import (
	// standard libraries
	"context"

	// external libraries
	"github.com/pkg/errors"

	// internal libraries
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/manager/state/store"
)

// TaskStatusLogger is an object that logs task status updates in a central
// place on the leader
type TaskStatusLogger struct {
	// tasks maps a task ID to its last known state so task updates are only
	// logged when the state changes
	tasks map[string]api.TaskState

	// we need a copy of the store to initialize the object state
	s *store.MemoryStore
}

func NewTaskStatusLogger(s *store.MemoryStore) *TaskStatusLogger {
	return &TaskStatusLogger{
		tasks: make(map[string]api.TaskState),
		s:     s,
	}
}

// Run runs the task status logger. First, it reads all of the current tasks
// out of the task database. Then, it starts an event loop to check for task
// status updates and logs them.
//
// Exits when the context is canceled
func (l *TaskStatusLogger) Run(ctx context.Context) error {
	ctx = log.WithField(ctx, "method", "(*TaskStatusLogger).Run")
	// we are going to initialize the state of the tasks map, and get an event
	// channel, so we don't miss any task updates. this should be pretty quick,
	// in and out.
	taskWatch, cancel, err := store.ViewAndWatch(l.s,
		func(tx store.ReadTx) error {
			tasks, err := store.FindTasks(tx, store.All)
			if err != nil {
				return err
			}
			for _, task := range tasks {
				l.tasks[task.ID] = task.Status.State
			}
			log.G(ctx).Debugf("initialized %v task statuses", len(tasks))
			return nil
		},
		api.EventUpdateTask{}, api.EventDeleteTask{},
	)
	if err != nil {
		return errors.Wrap(err, "error initializing task status logger")
	}

	// now the event loop
eventLoop:
	for {
		select {
		case ev := <-taskWatch:
			switch e := ev.(type) {
			case api.EventUpdateTask:
				// this is a rare case, i don't know how we'd get it, but we
				// have to program defensively. log it as a warning so that we
				// will know if it happens, and it'll be a sign that something
				// is inconsistent
				if e.Task == nil {
					log.G(ctx).Warn("got a task update event with a nil task")
					continue eventLoop
				}
				// so, here we're sploiting the fact that the 0-value of task
				// state is task state NEW. this means even if a task isn't in
				// the map because we haven't seen it yet, we still get the
				// correct state back, and we don't have to watch for task
				// creates
				state := l.tasks[e.Task.ID]
				if state != e.Task.Status.State {
					log.G(ctx).WithField("task.id", e.Task.ID).Infof(
						"task state update %v => %v",
						state, e.Task.Status.State,
					)
					// save the new task state value
					l.tasks[e.Task.ID] = e.Task.Status.State
				}
			case api.EventDeleteTask:
				// don't need to log here, but remove the task from our
				// tracking map so we don't leak memory.
				delete(l.tasks, e.Task.ID)
			}
		case <-ctx.Done():
			cancel()
			return nil
		}
	}
}
