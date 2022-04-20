package jobs

import (
	"context"
	"sync"
	"time"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator"
	"github.com/moby/swarmkit/v2/manager/orchestrator/restart"
	"github.com/moby/swarmkit/v2/manager/orchestrator/taskinit"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

// fakes_test.go is just a file to hold all of the test fakes used with the
// orchestrator, so that they don't pollute the test files

// fakeReconciler implements the reconciler interface for testing the
// orchestrator.
type fakeReconciler struct {
	sync.Mutex

	// serviceErrors contains a mapping of ids to errors that should be
	// returned if that ID is passed to reconcileService
	serviceErrors map[string]error

	// servicesReconciled is a list, in order, of all values this function has
	// been called with, including those that would return errors.
	servicesReconciled []string

	// servicesRelated is a set of all service IDs of services passed to
	// IsRelatedService
	servicesRelated []string
}

// ReconcileService implements the reconciler's ReconcileService method, but
// just records what arguments it has been passed, and maybe also returns an
// error if desired.
func (f *fakeReconciler) ReconcileService(id string) error {
	f.Lock()
	defer f.Unlock()
	f.servicesReconciled = append(f.servicesReconciled, id)
	if err, ok := f.serviceErrors[id]; ok {
		return err
	}
	return nil
}

func (f *fakeReconciler) getServicesReconciled() []string {
	f.Lock()
	defer f.Unlock()
	// we can't just return the slice, because then we'd be accessing it
	// outside of the protection of the mutex anyway. instead, we'll copy its
	// contents. this is fine because this is only the tests, and the slice is
	// almost certainly rather short.
	returnSet := make([]string, len(f.servicesReconciled))
	copy(returnSet, f.servicesReconciled)
	return returnSet
}

func (f *fakeReconciler) getRelatedServices() []string {
	f.Lock()
	defer f.Unlock()

	returnSet := make([]string, len(f.servicesRelated))
	copy(returnSet, f.servicesRelated)
	return returnSet
}

// finally, a few stubs to implement the InitHandler interface, just so types
// work out

func (f *fakeReconciler) IsRelatedService(s *api.Service) bool {
	f.Lock()
	defer f.Unlock()
	if s != nil {
		f.servicesRelated = append(f.servicesRelated, s.ID)
	}
	return true
}

func (f *fakeReconciler) FixTask(_ context.Context, _ *store.Batch, _ *api.Task) {}

func (f *fakeReconciler) SlotTuple(_ *api.Task) orchestrator.SlotTuple {
	return orchestrator.SlotTuple{}
}

// fakeRestartSupervisor implements the restart.SupervisorInterface interface.
// All of its methods are currently stubs, as it exists mostly to ensure that
// a real restart.Supervisor is not instantiated in the unit tests.
type fakeRestartSupervisor struct{}

func (f *fakeRestartSupervisor) Restart(_ context.Context, _ store.Tx, _ *api.Cluster, _ *api.Service, _ api.Task) error {
	return nil
}

func (f *fakeRestartSupervisor) UpdatableTasksInSlot(_ context.Context, _ orchestrator.Slot, _ *api.Service) orchestrator.Slot {
	return orchestrator.Slot{}
}

func (f *fakeRestartSupervisor) RecordRestartHistory(_ orchestrator.SlotTuple, _ *api.Task) {}

func (f *fakeRestartSupervisor) DelayStart(_ context.Context, _ store.Tx, _ *api.Task, _ string, _ time.Duration, _ bool) <-chan struct{} {
	return make(chan struct{})
}

func (f *fakeRestartSupervisor) StartNow(_ store.Tx, _ string) error {
	return nil
}

func (f *fakeRestartSupervisor) Cancel(_ string) {}

func (f *fakeRestartSupervisor) CancelAll() {}

func (f *fakeRestartSupervisor) ClearServiceHistory(_ string) {}

// fakeCheckTasksFunc is a function to use as checkTasksFunc when unit testing
// the orchestrator. it will create a service with ID fakeCheckTasksFuncCalled
// and call ih.IsRelatedService with that service, allowing a roundabout way
// to ensure it's been called.
func fakeCheckTasksFunc(_ context.Context, _ *store.MemoryStore, _ store.ReadTx, ih taskinit.InitHandler, _ restart.SupervisorInterface) error {
	ih.IsRelatedService(&api.Service{ID: "fakeCheckTasksFuncCalled"})
	return nil
}
