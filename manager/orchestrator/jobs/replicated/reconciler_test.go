package replicated

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/types"

	"context"
	"fmt"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator"
	"github.com/docker/swarmkit/manager/state/store"
)

type fakeRestartSupervisor struct {
	tasks []string
}

func (f *fakeRestartSupervisor) Restart(_ context.Context, _ store.Tx, _ *api.Cluster, _ *api.Service, task api.Task) error {
	f.tasks = append(f.tasks, task.ID)
	return nil
}

// uniqueSlotsMatcher is used to verify that a set of tasks all have unique,
// non-overlapping slot numbers
type uniqueSlotsMatcher struct {
	duplicatedSlot uint64
}

func (u uniqueSlotsMatcher) Match(actual interface{}) (bool, error) {
	tasks, ok := actual.([]*api.Task)
	if !ok {
		return false, fmt.Errorf("actual is not []*api.Tasks{}")
	}

	slots := map[uint64]bool{}
	for _, task := range tasks {
		if filled, ok := slots[task.Slot]; ok || filled {
			u.duplicatedSlot = task.Slot
			return false, nil
		}
		slots[task.Slot] = true
	}
	return true, nil
}

func (u uniqueSlotsMatcher) FailureMessage(_ interface{}) string {
	return fmt.Sprintf("expected tasks to have unique slots, but %v is duplicated", u.duplicatedSlot)
}

func (u uniqueSlotsMatcher) NegatedFailureMessage(_ interface{}) string {
	return fmt.Sprintf("expected tasks to have duplicate slots")
}

func HaveUniqueSlots() GomegaMatcher {
	return uniqueSlotsMatcher{}
}

func AllTasks(s *store.MemoryStore) []*api.Task {
	var tasks []*api.Task
	s.View(func(tx store.ReadTx) {
		tasks, _ = store.FindTasks(tx, store.All)
	})
	return tasks
}

var _ = Describe("Replicated Job reconciler", func() {
	var (
		r       *Reconciler
		s       *store.MemoryStore
		cluster *api.Cluster
		f       *fakeRestartSupervisor
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		Expect(s).ToNot(BeNil())
		f = &fakeRestartSupervisor{}

		r = &Reconciler{
			store:   s,
			restart: f,
		}
	})

	AfterEach(func() {
		s.Close()
	})

	Describe("ReconcileService", func() {
		When("reconciling a service", func() {
			var (
				serviceID        string
				service          *api.Service
				maxConcurrent    uint64
				totalCompletions uint64

				reconcileErr error
			)

			BeforeEach(func() {
				serviceID = "someService"
				maxConcurrent = 10
				totalCompletions = 30
				service = &api.Service{
					ID: serviceID,
					Spec: api.ServiceSpec{
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{
								MaxConcurrent:    maxConcurrent,
								TotalCompletions: totalCompletions,
							},
						},
					},
				}

				cluster = &api.Cluster{
					ID: "someCluster",
					Spec: api.ClusterSpec{
						Annotations: api.Annotations{
							Name: "someCluster",
						},
						TaskDefaults: api.TaskDefaults{
							LogDriver: &api.Driver{
								Name: "someDriver",
							},
						},
					},
				}

			})

			JustBeforeEach(func() {
				err := s.Update(func(tx store.Tx) error {
					if service != nil {
						if err := store.CreateService(tx, service); err != nil {
							return err
						}
					}
					if cluster != nil {
						return store.CreateCluster(tx, cluster)
					}
					return nil
				})
				Expect(err).ToNot(HaveOccurred())

				reconcileErr = r.ReconcileService(serviceID)
			})

			When("the job has no tasks yet created", func() {
				It("should create MaxConcurrent number of tasks", func() {
					tasks := AllTasks(s)
					// casting maxConcurrent to an int, which we know is safe
					// because we set its value ourselves.
					Expect(tasks).To(HaveLen(int(maxConcurrent)))

					for _, task := range tasks {
						Expect(task.ServiceID).To(Equal(service.ID))
						Expect(task.JobIteration).ToNot(BeNil())
						Expect(task.JobIteration.Index).To(Equal(uint64(0)))
					}
				})

				It("should assign each task to a unique slot", func() {
					tasks := AllTasks(s)

					Expect(tasks).To(HaveUniqueSlots())
				})

				It("should return no error", func() {
					Expect(reconcileErr).ToNot(HaveOccurred())
				})

				It("should set the desired state of each task to COMPLETE", func() {
					tasks := AllTasks(s)
					for _, task := range tasks {
						Expect(task.DesiredState).To(Equal(api.TaskStateCompleted))
					}
				})

				It("should use the cluster to set the default log driver", func() {
					tasks := AllTasks(s)
					Expect(len(tasks) >= 1).To(BeTrue())

					Expect(tasks[0].LogDriver).To(Equal(cluster.Spec.TaskDefaults.LogDriver))
				})
			})

			When("the job has some tasks already in progress", func() {
				BeforeEach(func() {
					s.Update(func(tx store.Tx) error {
						// create 6 tasks before we reconcile the service.
						// also, to fully exercise the slot picking code, we'll
						// assign these tasks to every other slot
						for i := uint64(0); i < 12; i += 2 {
							task := orchestrator.NewTask(cluster, service, i, "")
							task.JobIteration = &api.Version{}
							task.DesiredState = api.TaskStateCompleted

							if err := store.CreateTask(tx, task); err != nil {
								return err
							}
						}

						return nil
					})
				})

				It("should create only the number of tasks needed to reach MaxConcurrent", func() {
					tasks := AllTasks(s)

					Expect(tasks).To(HaveLen(int(maxConcurrent)))
				})

				It("should assign each new task to a unique slot", func() {
					tasks := AllTasks(s)
					Expect(tasks).To(HaveUniqueSlots())
				})
			})

			When("some running tasks are desired to be shutdown", func() {
				BeforeEach(func() {
					err := s.Update(func(tx store.Tx) error {
						for i := uint64(0); i < maxConcurrent; i++ {
							task := orchestrator.NewTask(cluster, service, i, "")
							task.JobIteration = &api.Version{}
							task.DesiredState = api.TaskStateShutdown

							if err := store.CreateTask(tx, task); err != nil {
								return err
							}
						}
						return nil
					})
					Expect(err).ToNot(HaveOccurred())
				})

				It("should ignore tasks shutting down when creating new ones", func() {
					tasks := AllTasks(s)
					Expect(tasks).To(HaveLen(int(maxConcurrent) * 2))

				})

				It("should reuse slots numbers", func() {
					tasks := AllTasks(s)
					Expect(tasks).ToNot(HaveUniqueSlots())
				})
			})

			When("a job has some failing and some completed tasks", func() {
				var (
					desiredNewTasks uint64
					failingTasks    []string
				)

				BeforeEach(func() {
					failingTasks = []string{}
					err := s.Update(func(tx store.Tx) error {
						// first, create a set of tasks with slots
						// [0, maxConcurrent-1] that have all succeeded
						for i := uint64(0); i < maxConcurrent; i++ {
							task := orchestrator.NewTask(cluster, service, i, "")
							task.JobIteration = &api.Version{}
							task.DesiredState = api.TaskStateCompleted
							task.Status.State = api.TaskStateCompleted
							if err := store.CreateTask(tx, task); err != nil {
								return err
							}
						}

						// next, create half of maxConcurrent tasks, all
						// failing.
						startSlot := maxConcurrent
						endSlot := startSlot + (maxConcurrent / 2)
						for i := startSlot; i < endSlot; i++ {
							task := orchestrator.NewTask(cluster, service, i, "")
							task.JobIteration = &api.Version{}
							task.DesiredState = api.TaskStateCompleted
							task.Status.State = api.TaskStateFailed
							failingTasks = append(failingTasks, task.ID)
							if err := store.CreateTask(tx, task); err != nil {
								return err
							}
						}

						// it might seem dumb to do this instead of just using
						// maxConcurrent / 2, but this avoids any issues with
						// the parity of maxConcurrent that might otherwise
						// arise from integer division. we want enough tasks to
						// get us up to maxConcurrent, including the ones
						// already extant and failing.
						desiredNewTasks = maxConcurrent - (maxConcurrent / 2)
						return nil
					})
					Expect(err).ToNot(HaveOccurred())
				})

				It("should not reuse slot numbers", func() {
					tasks := AllTasks(s)
					Expect(tasks).To(HaveUniqueSlots())
				})

				It("should not replace the failing tasks", func() {
					s.View(func(tx store.ReadTx) {
						// Get all tasks that are in desired state Completed
						tasks, err := store.FindTasks(tx, store.ByDesiredState(api.TaskStateCompleted))
						Expect(err).ToNot(HaveOccurred())

						// count the tasks that are currently active. use type
						// uint64 to make comparison with maxConcurrent easier.
						activeTasks := uint64(0)
						for _, task := range tasks {
							if task.Status.State != api.TaskStateCompleted {
								activeTasks++
							}
						}

						// Assert that there are maxConcurrent of these tasks
						Expect(activeTasks).To(Equal(maxConcurrent))

						// Now, assert that there are 1/2 maxConcurrent New
						// tasks. This shouldn't be a problem, but while we're
						// here we might as well do this sanity check
						var newTasks uint64
						for _, task := range tasks {
							if task.Status.State == api.TaskStateNew {
								newTasks++
							}
						}
						Expect(newTasks).To(Equal(desiredNewTasks))
					})
				})

				It("should call Restart for each failing task", func() {
					Expect(f.tasks).To(ConsistOf(failingTasks))
				})
			})

			When("a job is almost complete, and doesn't need MaxConcurrent tasks running", func() {
				BeforeEach(func() {
					// we need to create a rather large number of tasks, all in
					// COMPLETE state.
					err := s.Update(func(tx store.Tx) error {
						for i := uint64(0); i < totalCompletions-10; i++ {
							// each task will get a unique slot

							task := orchestrator.NewTask(nil, service, i, "")
							task.JobIteration = &api.Version{}
							task.Status.State = api.TaskStateCompleted
							task.DesiredState = api.TaskStateCompleted

							if err := store.CreateTask(tx, task); err != nil {
								return err
							}
						}
						return nil
					})

					Expect(err).ToNot(HaveOccurred())
				})

				It("should create no more than the tasks needed to reach TotalCompletions", func() {
					var newTasks []*api.Task
					s.View(func(tx store.ReadTx) {
						newTasks, _ = store.FindTasks(tx, store.ByTaskState(api.TaskStateNew))
					})

					Expect(newTasks).To(HaveLen(10))
				})

				It("should give each new task a unique slot", func() {
					tasks := AllTasks(s)

					Expect(tasks).To(HaveUniqueSlots())
				})
			})

			When("the service does not exist", func() {
				BeforeEach(func() {
					service = nil
				})

				It("should return no error", func() {
					Expect(reconcileErr).ToNot(HaveOccurred())
				})

				It("should create no tasks", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(BeEmpty())
					})
				})
			})
		})

		It("should return an underflow error if there are more running tasks than TotalCompletions", func() {
			// this is an error condition which should not happen in real life,
			// but i want to make sure that we can't accidentally start
			// creating nearly the maximum 64-bit unsigned int number of tasks.
			maxConcurrent := uint64(10)
			totalCompletions := uint64(20)
			err := s.Update(func(tx store.Tx) error {
				service := &api.Service{
					ID: "someService",
					Spec: api.ServiceSpec{
						Mode: &api.ServiceSpec_ReplicatedJob{
							ReplicatedJob: &api.ReplicatedJob{
								MaxConcurrent:    maxConcurrent,
								TotalCompletions: totalCompletions,
							},
						},
					},
				}
				if err := store.CreateService(tx, service); err != nil {
					return err
				}

				for i := uint64(0); i < totalCompletions+10; i++ {
					task := orchestrator.NewTask(nil, service, 0, "")
					task.JobIteration = &api.Version{}
					task.DesiredState = api.TaskStateCompleted

					if err := store.CreateTask(tx, task); err != nil {
						return err
					}
				}
				return nil
			})
			Expect(err).ToNot(HaveOccurred())

			reconcileErr := r.ReconcileService("someService")
			Expect(reconcileErr).To(HaveOccurred())
			Expect(reconcileErr.Error()).To(ContainSubstring("underflow"))
		})
	})
})
