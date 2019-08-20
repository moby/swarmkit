package replicatedjob

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator/testutils"
	"github.com/docker/swarmkit/manager/state/store"
)

// fakeReconciler implements the reconciler interface for testing the
// orchestrator.
type fakeReconciler struct {
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
	f.servicesReconciled = append(f.servicesReconciled, id)
	if err, ok := f.serviceErrors[id]; ok {
		return err
	}
	return nil
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
		It("should stop when Stop is called", func(done Done) {
			stopped := testutils.EnsureRuns(func() { o.Run(context.Background()) })
			o.Stop()
			Expect(stopped).To(BeClosed())
			close(done)
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
				if err := store.CreateService(tx, globalJob); err != nil {
					return err
				}

				cluster := &api.Cluster{
					ID: "someCluster",
					Spec: api.ClusterSpec{
						Annotations: api.Annotations{
							Name: "someName",
						},
					},
				}

				return store.CreateCluster(tx, cluster)
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

		It("should pick up the cluster object", func() {
			// this is a white-box test which looks to see that o.cluster is
			// set correctly.
			Expect(o.cluster).ToNot(BeNil())
			Expect(o.cluster.ID).To(Equal("someCluster"))
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
		It("should reconcile each time it receives an event", func() {
		})
	})
})
