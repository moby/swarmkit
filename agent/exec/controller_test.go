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

func TestRun(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Slightly contrived but it helps to keep the reporting straight.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared"),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting"),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started"),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed"),
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
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskPrepared),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "already prepared"),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started"),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed"),
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
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "already started"),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed"),
	)

	assert.NoError(t, Run(ctx, ctlr, reporter))
}

// TestRunStartedWhenStartedIdempotence
func TestRunStartedWhenStartedIdempotence(t *testing.T) {
	ctx, ctlr, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Do the same thing, but return from Start.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "already started"),
		ctlr.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted, "completed"),
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
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared"),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting"),
		ctlr.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning, "started").
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
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared"),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting, "starting"),
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
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing, "preparing"),
		ctlr.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady, "prepared").Do(
			func(ctx context.Context, state api.TaskState, msg string) error {
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
