package global

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/orchestrator"
	"github.com/moby/swarmkit/v2/manager/state/store"
)

type fakeRestartSupervisor struct {
	tasks []string
}

func (f *fakeRestartSupervisor) Restart(_ context.Context, _ store.Tx, _ *api.Cluster, _ *api.Service, task api.Task) error {
	f.tasks = append(f.tasks, task.ID)
	return nil
}

var _ = Describe("Global Job Reconciler", func() {
	var (
		r *Reconciler
		s *store.MemoryStore
		f *fakeRestartSupervisor
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
		var (
			serviceID string
			service   *api.Service
			cluster   *api.Cluster
			nodes     []*api.Node
			tasks     []*api.Task
		)

		BeforeEach(func() {
			serviceID = "someService"

			// Set up the service and nodes. We can change these later
			service = &api.Service{
				ID: serviceID,
				Spec: api.ServiceSpec{
					Mode: &api.ServiceSpec_GlobalJob{
						// GlobalJob has no parameters
						GlobalJob: &api.GlobalJob{},
					},
				},
				JobStatus: &api.JobStatus{},
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

			// at the beginning of each test, initialize tasks to be empty
			tasks = nil

			nodes = []*api.Node{
				{
					ID: "node1",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "name1",
						},
						Availability: api.NodeAvailabilityActive,
					},
					Status: api.NodeStatus{
						State: api.NodeStatus_READY,
					},
				},
				{
					ID: "node2",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "name2",
						},
						Availability: api.NodeAvailabilityActive,
					},
					Status: api.NodeStatus{
						State: api.NodeStatus_READY,
					},
				},
				{
					ID: "node3",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "name3",
						},
						Availability: api.NodeAvailabilityActive,
					},
					Status: api.NodeStatus{
						State: api.NodeStatus_READY,
					},
				},
			}
		})

		JustBeforeEach(func() {
			// Just before the test executes, we'll create all of the objects
			// needed by the test and execute reconcileService. If we need
			// to change these objects, then we can do so in BeforeEach blocks
			err := s.Update(func(tx store.Tx) error {
				if service != nil {
					if err := store.CreateService(tx, service); err != nil {
						return err
					}
				}

				if cluster != nil {
					if err := store.CreateCluster(tx, cluster); err != nil {
						return err
					}
				}

				for _, node := range nodes {
					if err := store.CreateNode(tx, node); err != nil {
						return err
					}
				}
				for _, task := range tasks {
					if err := store.CreateTask(tx, task); err != nil {
						return err
					}
				}

				return nil
			})

			Expect(err).ToNot(HaveOccurred())

			err = r.ReconcileService(serviceID)
			Expect(err).ToNot(HaveOccurred())
		})

		When("the service is updated", func() {
			var allTasks []*api.Task

			JustBeforeEach(func() {
				// this JustBeforeEach will run after the one where the service
				// etc is created and reconciled.
				err := s.Update(func(tx store.Tx) error {
					service := store.GetService(tx, serviceID)
					service.JobStatus.JobIteration.Index++
					service.Spec.Task.ForceUpdate++
					return store.UpdateService(tx, service)
				})
				Expect(err).ToNot(HaveOccurred())

				err = r.ReconcileService(serviceID)
				Expect(err).ToNot(HaveOccurred())

				s.View(func(tx store.ReadTx) {
					allTasks, err = store.FindTasks(tx, store.ByServiceID(serviceID))
				})
				Expect(err).ToNot(HaveOccurred())
			})

			It("should remove tasks belonging to old iterations", func() {
				count := 0
				for _, task := range allTasks {
					Expect(task.JobIteration).ToNot(BeNil())
					if task.JobIteration.Index == 0 {
						Expect(task.DesiredState).To(Equal(api.TaskStateRemove))
						count++
					}
				}
				Expect(count).To(Equal(len(nodes)))
			})

			It("should create new tasks with the new iteration", func() {
				count := 0
				for _, task := range allTasks {
					Expect(task.JobIteration).ToNot(BeNil())
					if task.JobIteration.Index == 1 {
						Expect(task.DesiredState).To(Equal(api.TaskStateCompleted))
						count++
					}
				}
				Expect(count).To(Equal(len(nodes)))
			})
		})

		When("there are failed tasks", func() {
			BeforeEach(func() {
				// all but the last node should be filled.
				for _, node := range nodes[:len(nodes)-1] {
					task := orchestrator.NewTask(cluster, service, 0, node.ID)
					task.JobIteration = &api.Version{}
					task.DesiredState = api.TaskStateCompleted
					task.Status.State = api.TaskStateFailed
					tasks = append(tasks, task)
				}
			})

			It("should not replace the failing tasks", func() {
				s.View(func(tx store.ReadTx) {
					tasks, err := store.FindTasks(tx, store.All)
					Expect(err).ToNot(HaveOccurred())
					Expect(tasks).To(HaveLen(len(nodes)))

					// this is a sanity check
					numFailedTasks := 0
					numNewTasks := 0
					for _, task := range tasks {
						if task.Status.State == api.TaskStateNew {
							numNewTasks++
						}
						if task.Status.State == api.TaskStateFailed {
							numFailedTasks++
						}
					}

					Expect(numNewTasks).To(Equal(1))
					Expect(numFailedTasks).To(Equal(len(nodes) - 1))
				})
			})

			It("should call the restartSupervisor with the failed task", func() {
				taskIDs := []string{}
				// all of the tasks in the list should be the failed tasks.
				for _, task := range tasks {
					taskIDs = append(taskIDs, task.ID)
				}
				Expect(f.tasks).To(ConsistOf(taskIDs))
			})
		})

		When("creating new tasks", func() {
			It("should create a task for each node", func() {
				s.View(func(tx store.ReadTx) {
					for _, node := range nodes {
						nodeTasks, err := store.FindTasks(tx, store.ByNodeID(node.ID))
						Expect(err).ToNot(HaveOccurred())
						Expect(nodeTasks).To(HaveLen(1))
					}

				})
			})

			It("should set the desired state of each new task to COMPLETE", func() {
				s.View(func(tx store.ReadTx) {
					tasks, err := store.FindTasks(tx, store.All)
					Expect(err).ToNot(HaveOccurred())
					for _, task := range tasks {
						Expect(task.DesiredState).To(Equal(api.TaskStateCompleted))
					}
				})
			})

			It("should pick up and use the cluster object", func() {
				s.View(func(tx store.ReadTx) {
					tasks, err := store.FindTasks(tx, store.All)
					Expect(err).ToNot(HaveOccurred())
					for _, task := range tasks {
						Expect(task.LogDriver).ToNot(BeNil())
						Expect(task.LogDriver.Name).To(Equal("someDriver"))
					}
				})
			})

			When("there are already existing tasks", func() {
				BeforeEach(func() {
					// create a random task for node 1 that's in state Running
					tasks = append(tasks, &api.Task{
						ID:           "existingTask1",
						Spec:         service.Spec.Task,
						ServiceID:    serviceID,
						NodeID:       "node1",
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStateRunning,
						},
						JobIteration: &api.Version{},
					})
					tasks = append(tasks, &api.Task{
						ID:           "existingTask2",
						Spec:         service.Spec.Task,
						ServiceID:    serviceID,
						NodeID:       "node2",
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStateCompleted,
						},
						JobIteration: &api.Version{},
					})
				})

				It("should only create tasks for nodes that need them", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(HaveLen(3))
						Expect(tasks).To(
							ContainElement(
								WithTransform(func(t *api.Task) string {
									return t.ID
								}, Equal("existingTask1")),
							),
						)
					})
				})
			})

			When("there are placement constraints", func() {
				// This isn't a rigorous test of whether every placement
				// constraint works. Constraints are handled by another
				// package, so we have no need to test that. We just need to be
				// sure that placement constraints are correctly checked and
				// used.

				BeforeEach(func() {
					// set a constraint on the task to be only node1
					service.Spec.Task.Placement = &api.Placement{
						Constraints: []string{"node.id==node1"},
					}
				})

				It("should only create tasks on nodes matching the constraints", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(HaveLen(1))
						Expect(tasks[0].NodeID).To(Equal("node1"))
					})
				})
			})

			When("the service no longer exists", func() {
				BeforeEach(func() {
					service = nil
				})

				It("should create no tasks", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(BeEmpty())
					})
				})
			})

			When("a node is drained or paused", func() {
				BeforeEach(func() {
					// set node1 to drain, set node2 to pause
					nodes[0].Spec.Availability = api.NodeAvailabilityDrain
					nodes[1].Spec.Availability = api.NodeAvailabilityPause
					tasks = append(tasks, &api.Task{
						ID:           "someID",
						ServiceID:    service.ID,
						NodeID:       nodes[0].ID,
						DesiredState: api.TaskStateCompleted,
						Status: api.TaskStatus{
							State: api.TaskStateRunning,
						},
						JobIteration: &api.Version{
							Index: service.JobStatus.JobIteration.Index,
						},
					})
				})

				It("should not create tasks for those nodes", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(HaveLen(2))

						node2Tasks, err := store.FindTasks(tx, store.ByNodeID(nodes[2].ID))
						Expect(err).ToNot(HaveOccurred())
						Expect(node2Tasks).To(HaveLen(1))
						Expect(node2Tasks[0].DesiredState).To(Equal(api.TaskStateCompleted))
					})
				})

				It("should shut down tasks on drained nodes", func() {
					s.View(func(tx store.ReadTx) {
						node0Tasks, err := store.FindTasks(tx, store.ByNodeID(nodes[0].ID))
						Expect(err).ToNot(HaveOccurred())
						Expect(node0Tasks[0].DesiredState).To(Equal(api.TaskStateShutdown))
					})
				})
			})

			When("a node is not READY", func() {
				BeforeEach(func() {
					nodes[0].Status.State = api.NodeStatus_DOWN
				})
				It("should not create tasks for that node", func() {
					s.View(func(tx store.ReadTx) {
						node0Tasks, err := store.FindTasks(tx, store.ByNodeID(nodes[0].ID))
						Expect(err).ToNot(HaveOccurred())
						Expect(node0Tasks).To(BeEmpty())

						for _, node := range nodes[1:] {
							tasks, err := store.FindTasks(tx, store.ByNodeID(node.ID))
							Expect(err).ToNot(HaveOccurred())
							Expect(tasks).To(HaveLen(1))
						}
					})
				})
			})
		})

	})

	Describe("FixTask", func() {
		It("should take no action if the task is already desired to be shutdown", func() {
			task := &api.Task{
				ID:           "someTask",
				NodeID:       "someNode",
				DesiredState: api.TaskStateShutdown,
				Status: api.TaskStatus{
					State: api.TaskStateShutdown,
				},
			}
			err := s.Update(func(tx store.Tx) error {
				return store.CreateTask(tx, task)
			})
			Expect(err).ToNot(HaveOccurred())

			err = s.Batch(func(batch *store.Batch) error {
				r.FixTask(context.Background(), batch, task)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should shut down the task if its node is no longer valid", func() {
			// we can cover this case by just not creating the node, as a nil
			// node is invalid and this isn't a test of InvalidNode
			task := &api.Task{
				ID:           "someTask",
				NodeID:       "someNode",
				DesiredState: api.TaskStateCompleted,
				Status: api.TaskStatus{
					State: api.TaskStateFailed,
				},
			}
			err := s.Update(func(tx store.Tx) error {
				return store.CreateTask(tx, task)
			})
			Expect(err).ToNot(HaveOccurred())

			err = s.Batch(func(batch *store.Batch) error {
				r.FixTask(context.Background(), batch, task)
				return nil
			})
			Expect(err).ToNot(HaveOccurred())
			s.View(func(tx store.ReadTx) {
				t := store.GetTask(tx, task.ID)
				Expect(t.DesiredState).To(Equal(api.TaskStateShutdown))
			})
		})
	})
})
