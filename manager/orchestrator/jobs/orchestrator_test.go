package jobs

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"
	"fmt"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator/testutils"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

var _ = Describe("Replicated job orchestrator", func() {
	var (
		o          *Orchestrator
		s          *store.MemoryStore
		replicated *fakeReconciler
		global     *fakeReconciler
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		o = NewOrchestrator(s)
		replicated = &fakeReconciler{
			serviceErrors: map[string]error{},
		}
		global = &fakeReconciler{
			serviceErrors: map[string]error{},
		}
		o.replicatedReconciler = replicated
		o.globalReconciler = global
		o.restartSupervisor = &fakeRestartSupervisor{}
		o.checkTasksFunc = fakeCheckTasksFunc
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
					serviceReplicated := &api.Service{
						ID: fmt.Sprintf("serviceReplicated%v", i),
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: fmt.Sprintf("serviceReplicated%v", i),
							},
							Mode: &api.ServiceSpec_ReplicatedJob{
								ReplicatedJob: &api.ReplicatedJob{},
							},
						},
					}

					serviceGlobal := &api.Service{
						ID: fmt.Sprintf("serviceGlobal%v", i),
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: fmt.Sprintf("serviceGlobal%v", i),
							},
							Mode: &api.ServiceSpec_GlobalJob{
								GlobalJob: &api.GlobalJob{},
							},
						},
					}

					if err := store.CreateService(tx, serviceReplicated); err != nil {
						return err
					}
					if err := store.CreateService(tx, serviceGlobal); err != nil {
						return err
					}
				}

				return nil
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
			Expect(replicated.servicesReconciled).To(ConsistOf(
				"serviceReplicated0", "serviceReplicated1", "serviceReplicated2",
			))
		})

		It("should reconcile each global job service that already exists", func() {
			Expect(global.servicesReconciled).To(ConsistOf(
				"serviceGlobal0", "serviceGlobal1", "serviceGlobal2",
			))
		})

		It("should call checkTasksFunc for both reconcilers", func() {
			Expect(global.servicesRelated).To(ConsistOf("fakeCheckTasksFuncCalled"))
			Expect(replicated.servicesRelated).To(ConsistOf("fakeCheckTasksFuncCalled"))
		})

		When("an error is encountered reconciling some service", func() {
			BeforeEach(func() {
				replicated.serviceErrors["errService"] = fmt.Errorf("someError")
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
				Expect(replicated.servicesReconciled).To(ConsistOf(
					"serviceReplicated0", "serviceReplicated1", "serviceReplicated2", "errService",
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

			Eventually(replicated.getServicesReconciled).Should(ConsistOf(
				"service0", "service1", "service2",
			))
		})

		It("should reconcile each global job service received", func() {
			err := s.Update(func(tx store.Tx) error {
				for i := 0; i < 3; i++ {
					service := &api.Service{
						ID: fmt.Sprintf("service%v", i),
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: fmt.Sprintf("service%v", i),
							},
							Mode: &api.ServiceSpec_GlobalJob{
								GlobalJob: &api.GlobalJob{},
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

			Eventually(global.getServicesReconciled).Should(ConsistOf(
				"service0", "service1", "service2",
			))
		})

		When("a service is deleted", func() {
			BeforeEach(func() {
				err := s.Update(func(tx store.Tx) error {
					service := &api.Service{
						ID: "serviceDelete",
						Spec: api.ServiceSpec{
							Annotations: api.Annotations{
								Name: "serviceDelete",
							},
							Mode: &api.ServiceSpec_ReplicatedJob{
								ReplicatedJob: &api.ReplicatedJob{
									MaxConcurrent:    1,
									TotalCompletions: 1,
								},
							},
						},
					}

					if err := store.CreateService(tx, service); err != nil {
						return err
					}

					// create some tasks, like the service was actually running
					task1 := &api.Task{
						ID:           "task1",
						ServiceID:    "serviceDelete",
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStateCompleted,
						},
					}

					task2 := &api.Task{
						ID:           "task2",
						ServiceID:    "serviceDelete",
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStateRunning,
						},
					}

					if err := store.CreateTask(tx, task1); err != nil {
						return err
					}
					return store.CreateTask(tx, task2)
				})

				Expect(err).NotTo(HaveOccurred())

				// wait for a pass through the reconciler
				Eventually(replicated.getServicesReconciled).Should(ConsistOf(
					"serviceDelete",
				))
			})

			It("should remove tasks when a service is deleted", func() {
				err := s.Update(func(tx store.Tx) error {
					return store.DeleteService(tx, "serviceDelete")
				})
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() []*api.Task {
					var tasks []*api.Task
					s.View(func(tx store.ReadTx) {
						tasks, _ = store.FindTasks(tx, store.ByServiceID("serviceDelete"))
					})
					return tasks
				}).Should(SatisfyAll(
					HaveLen(2),
					WithTransform(
						func(tasks []*api.Task) []api.TaskState {
							states := []api.TaskState{}
							for _, task := range tasks {
								states = append(states, task.DesiredState)
							}
							return states
						},
						ConsistOf(api.TaskStateCompleted, api.TaskStateCompleted),
					),
				))
			})
		})

		When("receiving task events", func() {
			BeforeEach(func() {
				service := &api.Service{
					ID: "service0",
					Spec: api.ServiceSpec{
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{},
						},
					},
				}

				err := s.Update(func(tx store.Tx) error {
					if err := store.CreateService(tx, service); err != nil {
						return err
					}

					task := &api.Task{
						ID:           "someTask",
						ServiceID:    "service0",
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStatePreparing,
						},
					}
					return store.CreateTask(tx, task)
				})
				Expect(err).ToNot(HaveOccurred())
				Eventually(replicated.getServicesReconciled).Should(ConsistOf(
					"service0",
				))
			})

			It("should reconcile the service of a task that has entered a terminal state", func() {
				err := s.Update(func(tx store.Tx) error {
					task := store.GetTask(tx, "someTask")
					task.Status.State = api.TaskStateFailed
					return store.UpdateTask(tx, task)
				})
				Expect(err).ToNot(HaveOccurred())

				// we will have service0 twice -- once from the setup, and
				// once from the reconciliation pass in the test.
				Eventually(replicated.getServicesReconciled).Should(ConsistOf(
					"service0", "service0",
				))
			})

			It("should ignore tasks that are not in a terminal state", func() {
				err := s.Update(func(tx store.Tx) error {
					task := store.GetTask(tx, "someTask")
					task.Status.State = api.TaskStateRunning
					return store.UpdateTask(tx, task)
				})
				Expect(err).ToNot(HaveOccurred())

				Consistently(replicated.getServicesReconciled).Should(ConsistOf(
					"service0",
				))
			})
		})

		It("should not reconcile anything after calling Stop", func() {
			err := s.Update(func(tx store.Tx) error {
				service := &api.Service{
					ID: "service0",
					Spec: api.ServiceSpec{
						Annotations: api.Annotations{
							Name: "service0",
						},
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{},
						},
					},
				}

				return store.CreateService(tx, service)
			})

			Expect(err).ToNot(HaveOccurred())

			Eventually(replicated.getServicesReconciled).Should(ConsistOf("service0"))

			o.Stop()

			err = s.Update(func(tx store.Tx) error {
				service := &api.Service{
					ID: "service1",
					Spec: api.ServiceSpec{
						Annotations: api.Annotations{
							Name: "service1",
						},
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{},
						},
					},
				}

				return store.CreateService(tx, service)
			})

			// service1 should never be reconciled.
			Consistently(replicated.getServicesReconciled).Should(ConsistOf("service0"))
		})
	})
})
