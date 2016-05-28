package exec

import (
	"errors"
	"runtime"
	"testing"

	"github.com/docker/swarm-v2/api"
	"github.com/docker/swarm-v2/log"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"golang.org/x/net/context"
)

//go:generate mockgen -package exec -destination controller_test.mock.go -source controller.go Controller Reporter

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
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(t, ctx, task, ctlr, &api.TaskStatus{
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
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(t, ctx, task, ctlr, &api.TaskStatus{
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
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateAccepted,
		Message: "accepted",
	})

	// Actually prepare the task.
	task.Status = *status

	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStatePreparing,
		Message: "preparing",
	})

	task.Status = *status

	checkDo(t, ctx, task, ctlr, &api.TaskStatus{
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
		ctlr.EXPECT().Wait(gomock.Any()),
	)

	dctlr := &decorateController{
		MockController: ctlr,
		cstatus: &api.ContainerStatus{
			ExitCode: 0,
		},
	}

	// Report starting
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status
	// wait and cancel
	checkDo(t, ctx, task, dctlr, &api.TaskStatus{
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
		ctlr.EXPECT().Wait(gomock.Any()).Return(&ExitError{
			Code: 1,
		}),
	)

	// Report starting
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status
	// wait and cancel
	checkDo(t, ctx, task, dctlr, &api.TaskStatus{
		State: api.TaskStateFailed,
		RuntimeStatus: &api.TaskStatus_Container{
			Container: &api.ContainerStatus{
				ExitCode: 1,
			},
		},
		Message: "failed",
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
		ctlr.EXPECT().Wait(gomock.Any()).Return(&ExitError{Code: 1}),
	)

	// Before we can move to running, we have to move to startin.
	status := checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateStarting,
		Message: "starting",
	})

	task.Status = *status

	// start the container
	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status

	status = checkDo(t, ctx, task, ctlr, &api.TaskStatus{
		State:   api.TaskStateRunning,
		Message: "started",
	})

	task.Status = *status

	// now take the real exit to test wait cancelling.
	checkDo(t, ctx, task, dctlr, &api.TaskStatus{
		State: api.TaskStateFailed,
		RuntimeStatus: &api.TaskStatus_Container{
			Container: &api.ContainerStatus{
				ExitCode: 1,
			},
		},
		Message: "failed",
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

	checkDo(t, ctx, task, ctlr, &api.TaskStatus{
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

func checkDo(t *testing.T, ctx context.Context, task *api.Task, ctlr Controller, expected *api.TaskStatus) *api.TaskStatus {
	status, err := Do(ctx, task, ctlr)
	assert.NoError(t, err)
	assert.Equal(t, expected, status)

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

func TestRun(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Slightly contrived but it helps to keep the reporting straight.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared", nil),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting", nil),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started", nil),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed", nil),
	)

	assert.NoError(t, Run(ctx, ctlr, reporter))
}

// TestRunPreparedIdempotence ensures we don't report errors when a task has already
// been prepared.
func TestRunPreparedIdempotence(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// We return ErrTaskPrepared from Prepare and make sure we have a
	// successful run. We skip reporting on "READY" and go right to starting
	// here.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskPrepared),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "already prepared", nil),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started", nil),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed", nil),
	)

	assert.NoError(t, Run(ctx, ctlr, reporter))
}

// TestRunStartedWhenPreparedIdempotence ensures we don't report errors when a task has
// already been started.
func TestRunStartedWhenPreparedIdempotence(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// First, we return ErrTaskStarted from Prepare and make sure we have a
	// successful run. We should report that we are running jump right to wait.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "already started", nil),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed", nil),
	)

	assert.NoError(t, Run(ctx, ctlr, reporter))
}

// TestRunStartedWhenStartedIdempotence
func TestRunStartedWhenStartedIdempotence(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Do the same thing, but return from Start.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "already started", nil),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed", nil),
	)

	assert.NoError(t, Run(ctx, ctlr, reporter))
}

// TestRunReportingError ensures that we stop the controller on errors from the
// reporter. Obviously, these reports aren't tied to IO or other unreliable
// information, but this is for stopping out of date tasks transitions.
func TestRunReportingError(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	errShouldPropagate := errors.New("test error")

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared", nil),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting", nil),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started", nil).
			Return(errShouldPropagate),
	)

	if err := Run(ctx, ctlr, reporter); err != errShouldPropagate {
		t.Fatalf("unexpected reporting error: %v", err)
	}
}

// TestRunControllerError ensures that we stop the controller on errors from the
// controller. For now, the behavior is pretty simplistic.
func TestRunControllerError(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	errShouldPropagate := errors.New("test error")

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared", nil),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting", nil),
		ctlr.EXPECT().Start(gomock.Any()).Return(errShouldPropagate),
	)

	if err := Run(ctx, ctlr, reporter); err != errShouldPropagate {
		t.Fatalf("unexpected reporting error: %v", err)
	}
}

func TestRunCancel(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	ctx, cancel := context.WithCancel(ctx)

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing", nil),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared", nil).Do(
			func(ctx context.Context, state api.TaskState, msg string, cstatus *api.ContainerStatus) error {
				// cancelling context ensures next report never happens.
				cancel()
				return nil
			},
		),
		// This call should happen, but the context gets cancelled afterwards.
	)

	if err := Run(ctx, ctlr, reporter); err != context.Canceled {
		t.Errorf("unexpected error on cancelled run: %v", err)
	}
}

func genRunTestEnv(t *testing.T) (context.Context, *MockController, *MockReporter, func()) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		mocks       = gomock.NewController(t)
		ctlr        = NewMockController(mocks)
		reporter    = NewMockReporter(mocks)
	)

	// Put test name into log messages. Awesome!
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		ctx = log.WithLogger(ctx, log.L.WithField("test", fn.Name()))
	}

	return ctx, ctlr, reporter, func() {
		cancel()
		mocks.Finish()

	}
}
