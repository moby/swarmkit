package exec

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	"github.com/docker/swarmkit/protobuf/ptypes"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

//go:generate mockgen -package exec -destination controller_test.mock.go -source controller.go Controller StatusReporter

func TestResolve(t *testing.T) {
	var (
		ctx      = context.Background()
		executor = &mockExecutor{}
		task     = newTestTask(t, api.TaskStateAssigned, api.TaskStateRunning)
	)

	_, status, err := Resolve(ctx, task, executor)
	assert.NoError(t, err)
	assert.Equal(t, api.TaskStateAccepted, status.State)
	assert.Equal(t, "accepted", status.Message)

	task.Status = *status
	// now, we get no status update.
	_, status, err = Resolve(ctx, task, executor)
	assert.NoError(t, err)
	assert.Equal(t, task.Status, *status)

	// now test an error causing rejection
	executor.err = errors.New("some error")
	task = newTestTask(t, api.TaskStateAssigned, api.TaskStateRunning)
	_, status, err = Resolve(ctx, task, executor)
	assert.Equal(t, executor.err, err)
	assert.Equal(t, api.TaskStateRejected, status.State)

	// on Resolve failure, tasks already started should be considered failed
	task = newTestTask(t, api.TaskStateStarting, api.TaskStateRunning)
	_, status, err = Resolve(ctx, task, executor)
	assert.Equal(t, executor.err, err)
	assert.Equal(t, api.TaskStateFailed, status.State)

	// on Resolve failure, tasks already in terminated state don't need update
	task = newTestTask(t, api.TaskStateCompleted, api.TaskStateRunning)
	_, status, err = Resolve(ctx, task, executor)
	assert.Equal(t, executor.err, err)
	assert.Equal(t, api.TaskStateCompleted, status.State)

	// task is now foobared, from a reporting perspective but we can now
	// resolve the controller for some reason. Ensure the task state isn't
	// touched.
	task.Status = *status
	executor.err = nil
	_, status, err = Resolve(ctx, task, executor)
	assert.NoError(t, err)
	assert.Equal(t, task.Status, *status)
}

func TestAcceptPrepare(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateAssigned, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()
	gomock.InOrder(
		ctlr.EXPECT().Prepare(gomock.Any()),
	)

	// Report acceptance.
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateReady,
		Message: "prepared",
	})
}

func TestPrepareAlready(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateAssigned, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()
	gomock.InOrder(
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskPrepared),
	)

	// Report acceptance.
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateReady,
		Message: "prepared",
	})
}

func TestPrepareFailure(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateAssigned, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()
	gomock.InOrder(
		ctlr.EXPECT().Prepare(gomock.Any()).Return(errors.New("test error")),
	)

	// Report acceptance.
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRejected,
		Message: "preparing",
		Err:     "test error",
	})
}

func TestReadyRunning(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateReady, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()

	gomock.InOrder(
		ctlr.EXPECT().Start(gomock.Any()),
		ctlr.EXPECT().Wait(gomock.Any()).Return(context.Canceled),
		ctlr.EXPECT().Wait(gomock.Any()),
	)

	dctlr := &decorateController{
		MockController: ctlr,
		cstatus: &api.ContainerStatus{
			ExitCode: 0,
		},
	}

	// Report starting
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status

	// resume waiting
	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	}, ErrTaskRetry)

	task.Status = *status
	// wait and cancel
	checkDo(ctx, t, task, dctlr, &api.TaskStatus{
		State:   api.TaskStateCompleted,
		Message: "finished",
		RuntimeStatus: &api.TaskStatus_Container{
			Container: &api.ContainerStatus{
				ExitCode: 0,
			},
		},
	})
}

func TestReadyRunningExitFailure(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateReady, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()

	dctlr := &decorateController{
		MockController: ctlr,
		cstatus: &api.ContainerStatus{
			ExitCode: 1,
		},
	}

	gomock.InOrder(
		ctlr.EXPECT().Start(gomock.Any()),
		ctlr.EXPECT().Wait(gomock.Any()).Return(newExitError(1)),
	)

	// Report starting
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status
	checkDo(ctx, t, task, dctlr, &api.TaskStatus{
		State: api.TaskStateFailed,
		RuntimeStatus: &api.TaskStatus_Container{
			Container: &api.ContainerStatus{
				ExitCode: 1,
			},
		},
		Message: "started",
		Err:     "test error, exit code=1",
	})
}

func TestAlreadyStarted(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateReady, api.TaskStateRunning)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()

	dctlr := &decorateController{
		MockController: ctlr,
		cstatus: &api.ContainerStatus{
			ExitCode: 1,
		},
	}

	gomock.InOrder(
		ctlr.EXPECT().Start(gomock.Any()).Return(ErrTaskStarted),
		ctlr.EXPECT().Wait(gomock.Any()).Return(context.Canceled),
		ctlr.EXPECT().Wait(gomock.Any()).Return(newExitError(1)),
	)

	// Before we can move to running, we have to move to startin.
	status := checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status

	status = checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	}, ErrTaskRetry)

	task.Status = *status

	// now take the real exit to test wait cancelling.
	checkDo(ctx, t, task, dctlr, &api.TaskStatus{
		State: api.TaskStateFailed,
		RuntimeStatus: &api.TaskStatus_Container{
			Container: &api.ContainerStatus{
				ExitCode: 1,
			},
		},
		Message: "started",
		Err:     "test error, exit code=1",
	})

}
func TestShutdown(t *testing.T) {
	var (
		task              = newTestTask(t, api.TaskStateNew, api.TaskStateShutdown)
		ctx, ctlr, finish = buildTestEnv(t, task)
	)
	defer finish()
	gomock.InOrder(
		ctlr.EXPECT().Shutdown(gomock.Any()),
	)

	checkDo(ctx, t, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateShutdown,
		Message: "shutdown",
	})
}

// decorateController let's us decorate the mock as a ContainerController.
//
// Sorry this is here but GoMock is pure garbage.
type decorateController struct {
	*MockController
	cstatus *api.ContainerStatus

	waitFn func(ctx context.Context) error
}

func (dc *decorateController) ContainerStatus(ctx context.Context) (*api.ContainerStatus, error) {
	return dc.cstatus, nil
}

type exitCoder struct {
	code int
}

func newExitError(code int) error { return &exitCoder{code} }

func (ec *exitCoder) Error() string { return fmt.Sprintf("test error, exit code=%v", ec.code) }
func (ec *exitCoder) ExitCode() int { return ec.code }

func checkDo(ctx context.Context, t *testing.T, task *api.Task, ctlr Controller, expected *api.TaskStatus, expectedErr ...error) *api.TaskStatus {
	status, err := Do(ctx, task, ctlr)
	if len(expectedErr) > 0 {
		assert.Equal(t, expectedErr[0], err)
	} else {
		assert.NoError(t, err)
	}

	if task.Status.Timestamp != nil {
		// crazy timestamp validation follows
		previous, err := ptypes.Timestamp(task.Status.Timestamp)
		assert.Nil(t, err)

		current, err := ptypes.Timestamp(status.Timestamp)
		assert.Nil(t, err)

		if current.Before(previous) {
			// ensure that the timestamp alwways proceeds forward
			t.Fatalf("timestamp must proceed forward: %v < %v", current, previous)
		}
	}

	// if the status and task.Status are different, make sure new timestamp is greater

	copy := status.Copy()
	copy.Timestamp = nil // don't check against timestamp
	assert.Equal(t, expected, copy)

	return status
}

func newTestTask(t *testing.T, state, desired api.TaskState) *api.Task {
	return &api.Task{
		ID: "test-task",
		Status: api.TaskStatus{
			State: state,
		},
		DesiredState: desired,
	}
}

func buildTestEnv(t *testing.T, task *api.Task) (context.Context, *MockController, func()) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		mocks       = gomock.NewController(t)
		ctlr        = NewMockController(mocks)
	)

	// Put test name into log messages. Awesome!
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		ctx = log.WithLogger(ctx, log.L.WithField("test", fn.Name()))
	}

	return ctx, ctlr, func() {
		cancel()
		mocks.Finish()
	}
}

type mockExecutor struct {
	Executor

	err error
}

func (m *mockExecutor) Controller(t *api.Task) (Controller, error) {
	return nil, m.err
}
