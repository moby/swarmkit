package scheduler

import (
	"errors"
	"sync"

	"github.com/docker/swarm-v2/scheduler/orchestrator"
	"github.com/docker/swarm-v2/scheduler/planner"
	"github.com/docker/swarm-v2/state"
)

var (
	// ErrRunning is returned when starting an already running scheduler
	ErrRunning = errors.New("scheduler is already running")

	// ErrNotRunning is returned when stopping a scheduler that is not running.
	ErrNotRunning = errors.New("scheduler is not running")
)

type Scheduler struct {
	started bool
	l       sync.Mutex

	orchestrator *orchestrator.Orchestrator
	planner      *planner.Planner
}

func New(store state.WatchableStore) *Scheduler {
	return &Scheduler{
		started:      false,
		orchestrator: orchestrator.New(store),
		planner:      planner.New(store),
	}
}

func (s *Scheduler) Start() error {
	s.l.Lock()
	defer s.l.Unlock()

	// Avoid double start.
	if s.started {
		return ErrRunning
	}

	go s.planner.Run()
	go s.orchestrator.Run()

	s.started = true
	return nil
}

func (s *Scheduler) Stop() error {
	s.l.Lock()
	defer s.l.Unlock()

	if !s.started {
		return ErrNotRunning
	}

	s.orchestrator.Stop()
	s.planner.Stop()

	s.started = false
	return nil
}
