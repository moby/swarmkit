package testutils

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/go-events"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state"
	"github.com/stretchr/testify/assert"
)

// WatchTaskCreate waits for a task to be created.
func WatchTaskCreate(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventCreateTask); ok {
				return task.Task
			}
			if _, ok := event.(state.EventUpdateTask); ok {
				assert.FailNow(t, "got EventUpdateTask when expecting EventCreateTask", fmt.Sprint(event))
			}
		case <-time.After(time.Second):
			assert.FailNow(t, "no task creation")
		}
	}
}

// WatchTaskUpdate waits for a task to be updated.
func WatchTaskUpdate(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventUpdateTask); ok {
				return task.Task
			}
			if _, ok := event.(state.EventCreateTask); ok {
				assert.FailNow(t, "got EventCreateTask when expecting EventUpdateTask", fmt.Sprint(event))
			}
		case <-time.After(time.Second):
			assert.FailNow(t, "no task update")
		}
	}
}

// WatchTaskDelete waits for a task to be deleted.
func WatchTaskDelete(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventDeleteTask); ok {
				return task.Task
			}
		case <-time.After(time.Second):
			assert.FailNow(t, "no task deletion")
		}
	}
}

// ExpectTaskUpdate fails the test if the next event is not a task update event.
func ExpectTaskUpdate(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventUpdateTask); !ok {
				assert.FailNow(t, "expected task update event, got", fmt.Sprint(event))
			}
			return
		case <-time.After(time.Second):
			assert.FailNow(t, "no task update event")
		}
	}
}

// ExpectDeleteService fails the test if the next event is not a task delete event.
func ExpectDeleteService(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventDeleteService); !ok {
				assert.FailNow(t, "expected service delete event, got", fmt.Sprint(event))
			}
			return
		case <-time.After(time.Second):
			assert.FailNow(t, "no service delete event")
		}
	}
}

// ExpectCommit fails the test if the next event is not a commit event.
func ExpectCommit(t *testing.T, watch chan events.Event) {
	for {
		select {
		case event := <-watch:
			if _, ok := event.(state.EventCommit); !ok {
				assert.FailNow(t, "expected commit event, got", fmt.Sprint(event))
			}
			return
		case <-time.After(time.Second):
			assert.FailNow(t, "no commit event")
		}
	}

}

// WatchShutdownTask fails the test if the next event is not a task having its
// desired state changed to Shutdown.
func WatchShutdownTask(t *testing.T, watch chan events.Event) *api.Task {
	for {
		select {
		case event := <-watch:
			if task, ok := event.(state.EventUpdateTask); ok && task.Task.DesiredState == api.TaskStateShutdown {
				return task.Task
			}
			if _, ok := event.(state.EventCreateTask); ok {
				assert.FailNow(t, "got EventCreateTask when expecting EventUpdateTask", fmt.Sprint(event))
			}
		case <-time.After(time.Second):
			assert.FailNow(t, "no task deletion")
		}
	}
}

// SkipEvents skips events for 200 ms.
// FIXME(aaronl): Is this really necessary?
func SkipEvents(t *testing.T, watch chan events.Event) {
	for {
		select {
		case <-watch:
		case <-time.After(200 * time.Millisecond):
			return
		}
	}
}
