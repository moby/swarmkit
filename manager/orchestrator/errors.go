package orchestrator

import (
	"errors"
)

var (
	errReplicatedOrchestratorStarted    = errors.New("ReplicatedOrchestrator: already started")
	errReplicatedOrchestratorNotStarted = errors.New("ReplicatedOrchestrator: not started")

	errGlobalOrchestratorStarted    = errors.New("GlobalOrchestrator: already started")
	errGlobalOrchestratorNotStarted = errors.New("GlobalOrchestrator: not started")

	errTaskReaperStarted    = errors.New("TaskReaper: already started")
	errTaskReaperNotStarted = errors.New("TaskReaper: not started")
)
