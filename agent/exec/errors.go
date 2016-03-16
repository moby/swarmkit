package exec

import (
	"errors"
	"fmt"
)

var (
	// ErrRuntimeUnsupported encountered when a task requires a runtime
	// unsupported by the executor.
	ErrRuntimeUnsupported = errors.New("exec: unsupported runtime")

	// ErrTaskPrepared is called if the task is already prepared.
	ErrTaskPrepared = errors.New("exec: task already prepared")

	// ErrTaskStarted can be returned from any operation that cannot be
	// performed because the task has already been started. This does not imply
	// that the task is running but rather that it is no longer valid to call
	// Start.
	ErrTaskStarted = errors.New("exec: task already started")

	// ErrTaskUpdateFailed is returned if a task runner update fails.
	ErrTaskUpdateFailed = errors.New("exec: task update failed")

	// ErrRunnerClosed returned when a task runner has been closed.
	ErrRunnerClosed = errors.New("exec: runner closed")
)

// ExitError is returned by runner methods after encountering an error after a
// task exits. It should require any data to report on a non-zero exit code.
type ExitError struct {
	Code  int
	Cause error
}

func (e *ExitError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("task: non-zero exit (%v): %v", e.Code, e.Cause)
	}

	return fmt.Sprintf("task: non-zero exit (%v)", e.Code)
}
