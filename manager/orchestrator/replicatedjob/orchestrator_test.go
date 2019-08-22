package replicatedjob

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	"fmt"
	"sync"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator/testutils"
	"github.com/docker/swarmkit/manager/state/store"
)

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

var _ = Describe("Replicated job orchestrator", func() {
	var (
		o *Orchestrator
		s *store.MemoryStore
		f *fakeReconciler
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		o = NewOrchestrator(s)
		f = &fakeReconciler{
			serviceErrors: map[string]error{},
		}
		o.reconciler = f
	})

	Describe("Starting and stopping", func() {
		It("should stop when Stop is called", func() {
			stopped := testutils.EnsureRuns(func() { o.Run(context.Background()) })
			o.Stop()
			// Eventually here will repeatedly run the matcher against the
			// argument. This means that we will keep checking if stopped is
			// closed until the test times out. Using Eventually instead of
			// Expect ensure we can't race on "stopped".
			Eventually(stopped).Should(BeClosed())
		})
	})

	Describe("initialization", func() {
		BeforeEach(func() {
			// Create some services. 3 replicated jobs and 1 of different
			// service mode
			err := s.Update(func(tx store.Tx) error {
				for i := 0; i < 3; i++ {
					service := &api.Service{
						ID: fmt.Sprintf("service%v", i),
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: fmt.Sprintf("service%v", i),
							},
							Mode: &api.ServiceSpec_ReplicatedJob{
								ReplicatedJob: &api.ReplicatedJob{},
							},
						},
					}

					if err := store.CreateService(tx, service); err != nil {
						return err
					}
				}

				globalJob := &api.Service{
					ID: "globalJob",
					Spec: api.ServiceSpec{
						Annotations: api.Annotations{
							Name: "globalJob",
						},
						Mode: &api.ServiceSpec_GlobalJob{
							GlobalJob: &api.GlobalJob{},
						},
					},
				}
				return store.CreateService(tx, globalJob)
			})

			Expect(err).ToNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			testutils.EnsureRuns(func() { o.Run(context.Background()) })

			// Run and then stop the orchestrator. This will cause it to
			// initialize, performing the consequent reconciliation pass, and
			// then immediately return and exit. Because the call to Stop
			// blocks until Run completes, and because the initialization is
			// not interruptible, this will cause only the initialization to
			// occur.
			o.Stop()
		})

		It("should reconcile each replicated job service that already exists", func() {
			Expect(f.servicesReconciled).To(ConsistOf(
				"service0", "service1", "service2",
			))
		})

		When("an error is encountered reconciling some service", func() {
			BeforeEach(func() {
				f.serviceErrors["errService"] = fmt.Errorf("someError")
				err := s.Update(func(tx store.Tx) error {
					errService := &api.Service{
						ID: "errService",
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: "errService",
							},
							Mode: &api.ServiceSpec_ReplicatedJob{
								ReplicatedJob: &api.ReplicatedJob{},
							},
						},
					}
					return store.CreateService(tx, errService)
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should continue reconciling other services", func() {
				Expect(f.servicesReconciled).To(ConsistOf(
					"service0", "service1", "service2", "errService",
				))
			})
		})
	})

	Describe("receiving events", func() {
		var stopped <-chan struct{}
		BeforeEach(func() {
			stopped = testutils.EnsureRuns(func() { o.Run(context.Background()) })
		})

		AfterEach(func() {
			// If a test needs to stop early, that's no problem, because
			// repeated calls to Stop have no effect.
			o.Stop()
			Eventually(stopped).Should(BeClosed())
		})

		It("should reconcile each replicated job service received", func() {
			// Create some services. Wait a moment, and then check that they
			// are reconciled.
			err := s.Update(func(tx store.Tx) error {
				for i := 0; i < 3; i++ {
					service := &api.Service{
						ID: fmt.Sprintf("service%v", i),
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: fmt.Sprintf("service%v", i),
							},
							Mode: &api.ServiceSpec_ReplicatedJob{
								ReplicatedJob: &api.ReplicatedJob{},
							},
						},
					}

					if err := store.CreateService(tx, service); err != nil {
						return err
					}
				}
				return nil
			})
			Expect(err).ToNot(HaveOccurred())

			Eventually(f.getServicesReconciled).Should(ConsistOf(
				"service0", "service1", "service2",
			))
		})

		It("should not reconcile anything after calling Stop", func() {
			err := s.Update(func(tx store.Tx) error {
				service := &api.Service{
					ID: fmt.Sprintf("service0"),
					Spec: api.ServiceSpec{
						Annotations: api.Annotations{
							Name: fmt.Sprintf("service0"),
						},
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{},
						},
					},
				}

				return store.CreateService(tx, service)
			})

			Expect(err).ToNot(HaveOccurred())

			Eventually(f.getServicesReconciled).Should(ConsistOf("service0"))

			o.Stop()

			err = s.Update(func(tx store.Tx) error {
				service := &api.Service{
					ID: fmt.Sprintf("service1"),
					Spec: api.ServiceSpec{
						Annotations: api.Annotations{
							Name: fmt.Sprintf("service1"),
						},
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{},
						},
					},
				}

				return store.CreateService(tx, service)
			})

			// service1 should never be reconciled.
			Consistently(f.getServicesReconciled).Should(ConsistOf("service0"))
		})
	})
})
