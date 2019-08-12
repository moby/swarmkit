package globaljob

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"time"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
	gogotypes "github.com/gogo/protobuf/types"
)

var _ = Describe("Global Job Orchestrator", func() {
	var (
		o *Orchestrator
		s *store.MemoryStore
	)

	BeforeEach(func() {
		s = store.NewMemoryStore(nil)
		Expect(s).ToNot(BeNil())

		o = NewOrchestrator(s)
	})

	AfterEach(func() {
		s.Close()
	})

	Describe("reconcileService", func() {
		var (
			serviceID string
			service   *api.Service
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
				JobStatus: &api.JobStatus{
					LastExecution: gogotypes.TimestampNow(),
				},
			}

			// at the beginning of each test, initialize tasks to be empty
			tasks = nil

			// the Meta on nodes is not set automatically in tests, as setting
			// the meta requires passing a Proposer to the memory store, which
			// we do not do. in order to make these tests succeed, we will set
			// the node creation time to be 10 seconds ago
			tenSecondsAgoProto, err := gogotypes.TimestampProto(time.Now().Add(-10 * time.Second))
			Expect(err).ToNot(HaveOccurred())
			nodes = []*api.Node{
				{
					ID: "node1",
					Meta: api.Meta{
						CreatedAt: tenSecondsAgoProto,
					},
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
					Meta: api.Meta{
						CreatedAt: tenSecondsAgoProto,
					},
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
					Meta: api.Meta{
						CreatedAt: tenSecondsAgoProto,
					},
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

			err = o.reconcileService(serviceID)
			Expect(err).ToNot(HaveOccurred())
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

			When("nodes have been added since the job was started", func() {
				BeforeEach(func() {
					// Before this test, change the creation time of "node0" to
					// be 10 seconds from now. This should result in no task
					// being created for this node
					tenSecondsFromNowProto, err := gogotypes.TimestampProto(
						time.Now().Add(10 * time.Second),
					)
					Expect(err).ToNot(HaveOccurred())
					nodes[0].Meta.CreatedAt = tenSecondsFromNowProto
				})
				It("should not create tasks for those new nodes", func() {
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
				})

				It("should not create tasks for those nodes", func() {
					s.View(func(tx store.ReadTx) {
						tasks, err := store.FindTasks(tx, store.All)
						Expect(err).ToNot(HaveOccurred())
						Expect(tasks).To(HaveLen(1))
						Expect(tasks[0].NodeID).To(Equal("node3"))
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
})
