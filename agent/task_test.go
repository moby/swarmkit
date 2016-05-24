package agent

import (
	"testing"
	"time"

	"github.com/Sirupsen/logrus"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

func init() {
	logrus.SetLevel(logrus.DebugLevel)
}

func TestOperation(t *testing.T) {
	ctx := context.Background()
	task := &api.Task{
		Status:       api.TaskStatus{},
		DesiredState: api.TaskStateRunning,
	}
	remove := make(chan struct{})
	shutdown := make(chan struct{})

	tm := newTaskManager(ctx, task, &controllerStub{t: t}, statusReporterFunc(func(ctx context.Context, taskID string, status *api.TaskStatus) error {
		t.Log("status", status, status.State == api.TaskStateCompleted)
		if status.State == api.TaskStateCompleted {
			close(remove)
		} else if status.State == api.TaskStateDead {
			close(shutdown)
		}
		return nil
	}))

	// let's timeout this test, as any delay represents a problem with
	// progressing the state machine.
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	for {
		select {
		case <-remove:
			err := tm.Remove(ctx)

			if err != ErrRemoving {
				t.Fatal(err) // should be removing
			}
			// call it again, we should be removing.

			assert.Equal(t, ErrRemoving, tm.Remove(ctx))

			remove = nil // don't retry
		case <-shutdown:
			assert.NoError(t, tm.Close())
			shutdown = nil
		case <-tm.closed:
			return
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

type controllerStub struct {
	t *testing.T
	exec.Controller
}

func (cs *controllerStub) Prepare(ctx context.Context) error {
	cs.t.Log("(*controllerStub).Prepare")
	return nil
}

func (cs *controllerStub) Start(ctx context.Context) error {
	cs.t.Log("(*controllerStub).Start")
	return nil
}

func (cs *controllerStub) Wait(ctx context.Context) error {
	cs.t.Log("(*controllerStub).Wait")
	return nil
}

func (cs *controllerStub) Remove(ctx context.Context) error {
	cs.t.Log("(*controllerStub).Remove")
	return nil
}

type statusReporterFunc func(ctx context.Context, taskID string, status *api.TaskStatus) error

func (fn statusReporterFunc) UpdateTaskStatus(ctx context.Context, taskID string, status *api.TaskStatus) error {
	return fn(ctx, taskID, status)
}
