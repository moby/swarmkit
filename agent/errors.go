package agent

import (
	"errors"
	"fmt"
)

var (
	// ErrClosed is returned when an operation fails because the resource is closed.
	ErrClosed = errors.New("agent: closed")

	errNodeNotRegistered = fmt.Errorf("node not registered")

	errAgentNotStarted = errors.New("agent: not started")
	errAgentStarted    = errors.New("agent: already started")
	errAgentStopped    = errors.New("agent: stopped")

	errTaskNoContoller          = errors.New("agent: no task controller")
	errTaskNotAssigned          = errors.New("agent: task not assigned")
	errTaskStatusUpdateNoChange = errors.New("agent: no change in task status")
	errTaskDead                 = errors.New("agent: task dead")
	errTaskUnknown              = errors.New("agent: task unknown")

	ErrRemoving = errors.New("task: removing")

	errTaskInvalid = errors.New("task: invalid")
)
