package scheduler

import (
	"errors"

	"github.com/docker/swarm-v2/orchestrator"
	"github.com/docker/swarm-v2/planner"
	"github.com/docker/swarm-v2/state"
)

var (
	// ErrRunning is returned when starting an already running scheduler
	ErrRunning = errors.New("scheduler is already running")

	// ErrNotRunning is returned when stopping a scheduler that is not running.
	ErrNotRunning = errors.New("scheduler is not running")
)

// Scheduler is the component responsible for desired state convergence.
// It's composed of a series of subsystems such as the orchestrator, planner,
// and the allocator.
// The Scheduler type is responsible for subsystem coordination.
type Scheduler struct {
	started bool

	orchestrator *orchestrator.Orchestrator
	planner      *planner.Planner
}

// New creates a new Scheduler.
func New(store state.WatchableStore) *Scheduler {
	return &Scheduler{
		started:      false,
		orchestrator: orchestrator.New(store),
		planner:      planner.New(store),
	}
}

// Start starts all subsystems of the scheduler.
// Returns ErrRunning if called while the Scheduler is already running
func (s *Scheduler) Start() error {
	// Avoid double start.
	if s.started {
		return ErrRunning
	}

	go func() {
		// TODO(aaronl): The scheduler should have some kind of error
		// handling.
		_ = s.planner.Run()
	}()
	go s.orchestrator.Run()

	s.started = true
	return nil
}

// Stop stops all susbystems of the scheduler.
// Returns ErrNotRunning if called while the Scheduler is not running.
func (s *Scheduler) Stop() error {
	if !s.started {
		return ErrNotRunning
	}

	s.orchestrator.Stop()
	s.planner.Stop()

	s.started = false
	return nil
}
