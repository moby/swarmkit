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

//go:generate mockgen -package exec -destination runner_test.mock.go -source runner.go Runner

func TestRun(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Slightly contrived but it helps to keep the reporting straight.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting),
		runner.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning),
		runner.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted),
	)

	assert.NoError(t, Run(ctx, runner, reporter))
}

// TestRunPreparedIdempotence ensures we don't report errors when a task has already
// been prepared.
func TestRunPreparedIdempotence(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	// We return ErrTaskPrepared from Prepare and make sure we have a
	// succesful run. We skip reporting on "READY" and go right to starting
	// here.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()).Return(ErrTaskPrepared),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting),
		runner.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning),
		runner.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted),
	)

	assert.NoError(t, Run(ctx, runner, reporter))
}

// TestRunStartedWhenPreparedIdempotence ensures we don't report errors when a task has
// already been started.
func TestRunStartedWhenPreparedIdempotence(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	// First, we return ErrTaskStarted from Prepare and make sure we have a
	// succesful run. We should report that we are running jump right to wait.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning),
		runner.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted),
	)

	assert.NoError(t, Run(ctx, runner, reporter))
}

// TestRunStartedWhenStartedIdempotence
func TestRunStartedWhenStartedIdempotence(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	// Do the same thing, but return from Start.
	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting),
		runner.EXPECT().Start(gomock.Any()).Return(ErrTaskStarted),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning),
		runner.EXPECT().Wait(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateCompleted),
	)

	assert.NoError(t, Run(ctx, runner, reporter))
}

// TestRunReportingError ensures that we stop the runner on errors from the
// reporter. Obviously, these reports aren't tied to IO or other unreliable
// information, but this is for stopping out of date tasks transitions.
func TestRunReportingError(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	errShouldPropagate := errors.New("test error")

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting),
		runner.EXPECT().Start(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateRunning).
			Return(errShouldPropagate),
	)

	if err := Run(ctx, runner, reporter); err != errShouldPropagate {
		t.Fatalf("unexpected reporting error: %v", err)
	}
}

// TestRunRunnerError ensures that we stop the runner on errors from the
// runner. For now, the behavior is pretty simplistic.
func TestRunRunnerError(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	errShouldPropagate := errors.New("test error")

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateStarting),
		runner.EXPECT().Start(gomock.Any()).Return(errShouldPropagate),
	)

	if err := Run(ctx, runner, reporter); err != errShouldPropagate {
		t.Fatalf("unexpected reporting error: %v", err)
	}
}

func TestRunCancel(t *testing.T) {
	ctx, runner, reporter, finish := genRunTestEnv(t)
	defer finish()

	ctx, cancel := context.WithCancel(ctx)

	gomock.InOrder(
		reporter.EXPECT().Report(gomock.Any(), api.TaskStatePreparing),
		runner.EXPECT().Prepare(gomock.Any()),
		reporter.EXPECT().Report(gomock.Any(), api.TaskStateReady).Do(
			func(ctx context.Context, state api.TaskState) error {
				// cancelling context ensures next report never happens.
				cancel()
				return nil
			},
		),
		// This call should happen, but the context gets cancelled afterwards.
	)

	if err := Run(ctx, runner, reporter); err != context.Canceled {
		t.Errorf("unexpected error on cancelled run: %v", err)
	}
}

func genRunTestEnv(t *testing.T) (context.Context, *MockRunner, *MockReporter, func()) {
	var (
		ctx, cancel = context.WithCancel(context.Background())
		mocks       = gomock.NewController(t)
		runner      = NewMockRunner(mocks)
		reporter    = NewMockReporter(mocks)
	)

	// Put test name into log messages. Awesome!
	pc, _, _, ok := runtime.Caller(1)
	if ok {
		fn := runtime.FuncForPC(pc)
		ctx = log.WithLogger(ctx, log.L.WithField("test", fn.Name()))
	}

	return ctx, runner, reporter, func() {
		cancel()
		mocks.Finish()

	}
}
