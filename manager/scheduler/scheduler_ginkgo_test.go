package scheduler

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/orchestrator/testutils"
	"github.com/docker/swarmkit/manager/state/store"
)

// scheduler_ginkgo_test.go contains ginkgo BDD tests of the swarmkit scheduler

var _ = Describe("Scheduler", func() {
	var (
		ctx   context.Context
		s     *store.MemoryStore
		sched *Scheduler

		// these slices contain objects that will be added to the store before
		// each test run. this allows tests to simply append their required
		// fixtures to these slices
		nodes    []*api.Node
		tasks    []*api.Task
		services []*api.Service
		volumes  []*api.Volume
	)

	BeforeEach(func() {
		// create a context to use with the scheduler
		ctx = context.Background()

		nodes = []*api.Node{}
		tasks = []*api.Task{}
		services = []*api.Service{}
		volumes = []*api.Volume{}

		s = store.NewMemoryStore(nil)
		Expect(s).ToNot(BeNil())
		sched = New(s)
	})

	Describe("running", func() {
		var (
			schedErr     error
			schedStopped <-chan struct{}

			node    *api.Node
			service *api.Service
			task    *api.Task
		)

		// All tests of the running scheduler are done in this context. This
		// allows other tests of the scheduler to be executed on non-moving
		// parts.
		JustBeforeEach(func() {
			// launch the scheduler in its own goroutine, using EnsureRuns to make
			// sure the go scheduler can't bamboozle us and not execute it.
			schedStopped = testutils.EnsureRuns(func() {
				schedErr = sched.Run(ctx)
			})
		})

		BeforeEach(func() {
			node = &api.Node{
				ID: "nodeID1",
				Description: &api.NodeDescription{
					CSIInfo: []*api.NodeCSIInfo{
						{
							PluginName: "somePlug",
							NodeID:     "nodeCSI1",
						},
					},
				},
				Status: api.NodeStatus{
					State: api.NodeStatus_READY,
				},
			}
			nodes = append(nodes, node)

			// we need to create a service, because if we retry scheduling and
			// the service for a given task does not exist, we will remove that
			// task and not retry it.
			service = &api.Service{
				ID: "service1",
				Spec: api.ServiceSpec{
					Annotations: api.Annotations{
						Name: "service",
					},
					Task: api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.ContainerSpec{
								Mounts: []api.Mount{
									{
										Type:   api.MountTypeCSI,
										Source: "volume1",
										Target: "/var/",
									},
								},
							},
						},
					},
				},
			}
			services = append(services, service)

			task = &api.Task{
				ID:           "task1",
				ServiceID:    service.ID,
				DesiredState: api.TaskStateRunning,
				Status: api.TaskStatus{
					State: api.TaskStatePending,
				},
				Spec: service.Spec.Task,
			}

			tasks = append(tasks, task)
		})

		Context("Global mode tasks", func() {
			BeforeEach(func() {
				service.Spec.Mode = &api.ServiceSpec_Global{
					Global: &api.GlobalService{},
				}
				task.NodeID = node.ID
			})

			It("should still choose a volume for the task", func() {
				volume := &api.Volume{
					ID: "volumeID1",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume1",
						},
						Driver: &api.Driver{
							Name: "somePlug",
						},
						AccessMode: &api.VolumeAccessMode{
							Scope:   api.VolumeScopeSingleNode,
							Sharing: api.VolumeSharingNone,
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeID: "csi1",
					},
				}
				// add a volume
				err := s.Update(func(tx store.Tx) error {
					return store.CreateVolume(tx, volume)
				})
				Expect(err).ToNot(HaveOccurred())

				// now update the volume we need to update the
				// volume because we don't handle volumes at creation time.
				err = s.Update(func(tx store.Tx) error {
					v := store.GetVolume(tx, volume.ID)
					return store.UpdateVolume(tx, v)
				})
				Expect(err).ToNot(HaveOccurred())

				pollStore := func() *api.Task {
					var t *api.Task
					s.View(func(tx store.ReadTx) {
						t = store.GetTask(tx, task.ID)
					})
					return t
				}

				Eventually(pollStore, 10*time.Second).Should(
					SatisfyAll(
						WithTransform(
							func(t *api.Task) api.TaskState {
								return t.Status.State
							},
							Equal(api.TaskStateAssigned),
						),
						WithTransform(
							func(t *api.Task) []*api.VolumeAttachment {
								return t.Volumes
							},
							SatisfyAll(
								Not(BeNil()),
								ConsistOf(
									&api.VolumeAttachment{
										ID:     "volumeID1",
										Source: "volume1",
										Target: "/var/",
									},
								),
							),
						),
					),
				)
			})

			It("should not progress if no volume can be found", func() {
				pollStore := func() *api.Task {
					var t *api.Task
					s.View(func(tx store.ReadTx) {
						t = store.GetTask(tx, task.ID)
					})
					return t
				}

				Consistently(pollStore, 10*time.Second).Should(
					WithTransform(
						func(t *api.Task) api.TaskState {
							return t.Status.State
						},
						Equal(api.TaskStatePending),
					),
				)
			})
		})

		It("should choose volumes for tasks", func() {
			volume := &api.Volume{
				ID: "volumeID1",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volume1",
					},
					Driver: &api.Driver{
						Name: "somePlug",
					},
					AccessMode: &api.VolumeAccessMode{
						Scope:   api.VolumeScopeSingleNode,
						Sharing: api.VolumeSharingNone,
					},
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeID: "csi1",
				},
			}

			// add a volume
			err := s.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, volume)
			})
			Expect(err).ToNot(HaveOccurred())

			// now update the volume we need to update the
			// volume because we don't handle volumes at creation time.
			err = s.Update(func(tx store.Tx) error {
				v := store.GetVolume(tx, volume.ID)
				return store.UpdateVolume(tx, v)
			})
			Expect(err).ToNot(HaveOccurred())

			pollStore := func() *api.Task {
				var t *api.Task
				s.View(func(tx store.ReadTx) {
					t = store.GetTask(tx, task.ID)
				})
				return t
			}

			Eventually(pollStore, 10*time.Second).Should(
				SatisfyAll(
					WithTransform(
						func(t *api.Task) string {
							return t.NodeID
						},
						Equal(node.ID),
					),
					WithTransform(
						func(t *api.Task) []*api.VolumeAttachment {
							return t.Volumes
						},
						SatisfyAll(
							Not(BeNil()),
							ConsistOf(
								&api.VolumeAttachment{
									ID:     "volumeID1",
									Source: "volume1",
									Target: "/var/",
								},
							),
						),
					),
					WithTransform(
						func(t *api.Task) api.TaskState {
							return t.Status.State
						},
						Equal(api.TaskStateAssigned),
					),
				),
			)
		})

		It("should not commit a task if it does not have a volume", func() {
			pollStore := func() *api.Task {
				var t *api.Task
				s.View(func(tx store.ReadTx) {
					t = store.GetTask(tx, task.ID)
				})
				return t
			}

			Consistently(pollStore, 10*time.Second).Should(
				WithTransform(
					func(t *api.Task) api.TaskState {
						return t.Status.State
					},
					Equal(api.TaskStatePending),
				),
			)
		})

		AfterEach(func() {
			sched.Stop()
			Eventually(schedStopped).Should(BeClosed())
			Expect(schedErr).ToNot(HaveOccurred())
			s.Close()
		})
	})

	JustBeforeEach(func() {
		// add the nodes and tasks to the store
		err := s.Update(func(tx store.Tx) error {
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

			for _, service := range services {
				if err := store.CreateService(tx, service); err != nil {
					return err
				}
			}

			for _, volume := range volumes {
				if err := store.CreateVolume(tx, volume); err != nil {
					return err
				}
			}

			return nil
		})
		Expect(err).ToNot(HaveOccurred())
	})

	Describe("initialization", func() {
		BeforeEach(func() {
			cannedNode := func(i int) *api.Node {
				return &api.Node{
					ID: fmt.Sprintf("nodeID%d", i),
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: fmt.Sprintf("node%d", i),
						},
					},
					Description: &api.NodeDescription{
						Hostname: fmt.Sprintf("nodeHost%d", i),
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "somePlug",
								NodeID:     fmt.Sprintf("nodeCSI%d", i),
							},
						},
					},
					Status: api.NodeStatus{
						State: api.NodeStatus_READY,
					},
				}
			}

			nodes = append(nodes,
				cannedNode(0), cannedNode(1), cannedNode(2),
			)

			volumes = append(volumes,
				&api.Volume{
					ID: "volumeID1",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume1",
						},
						Group: "group1",
						Driver: &api.Driver{
							Name: "somePlug",
						},
						AccessMode: &api.VolumeAccessMode{
							Scope:   api.VolumeScopeMultiNode,
							Sharing: api.VolumeSharingAll,
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeID: "csi1",
					},
				},
				&api.Volume{
					ID: "volumeID2",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume2",
						},
						Group: "group2",
						Driver: &api.Driver{
							Name: "somePlug",
						},
						AccessMode: &api.VolumeAccessMode{
							Scope:   api.VolumeScopeSingleNode,
							Sharing: api.VolumeSharingNone,
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeID: "csi2",
					},
				},
				&api.Volume{
					ID: "volumeID3",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume3",
						},
						Group: "group2",
						Driver: &api.Driver{
							Name: "somePlug",
						},
						AccessMode: &api.VolumeAccessMode{
							Scope:   api.VolumeScopeSingleNode,
							Sharing: api.VolumeSharingNone,
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeID: "csi3",
					},
				},
			)

			tasks = append(tasks,
				&api.Task{
					ID:     "runningTask",
					NodeID: "nodeID0",
					Status: api.TaskStatus{
						State: api.TaskStateRunning,
					},
					DesiredState: api.TaskStateRunning,
					Spec: api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.ContainerSpec{
								Mounts: []api.Mount{
									{
										Type:   api.MountTypeCSI,
										Source: "volume1",
										Target: "/var/",
									},
									{
										Type:   api.MountTypeCSI,
										Source: "group:group2",
										Target: "/home/",
									},
								},
							},
						},
					},
					Volumes: []*api.VolumeAttachment{
						{
							Source: "volume1",
							Target: "/var/",
							ID:     "volumeID1",
						}, {
							Source: "group:group2",
							Target: "/home/",
							ID:     "volumeID3",
						},
					},
				},
				&api.Task{
					ID:     "shutdownTask",
					NodeID: "nodeID1",
					Status: api.TaskStatus{
						State: api.TaskStateShutdown,
					},
					DesiredState: api.TaskStateShutdown,
					Spec: api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.ContainerSpec{
								Mounts: []api.Mount{
									{
										Type:   api.MountTypeCSI,
										Source: "volume1",
										Target: "/foo/",
									},
								},
							},
						},
					},
					Volumes: []*api.VolumeAttachment{
						{
							Source: "volume1",
							Target: "/foo/",
							ID:     "volumeID1",
						},
					},
				},
				&api.Task{
					ID:     "pendingID",
					NodeID: "nodeID2",
					Status: api.TaskStatus{
						State: api.TaskStatePending,
					},
					DesiredState: api.TaskStateRunning,
					Spec: api.TaskSpec{
						Runtime: &api.TaskSpec_Container{
							Container: &api.ContainerSpec{
								Mounts: []api.Mount{
									{
										Type:   api.MountTypeCSI,
										Source: "group:group2",
										Target: "/foo/",
									},
								},
							},
						},
					},
				},
			)
		})

		JustBeforeEach(func() {
			var err error
			s.View(func(tx store.ReadTx) {
				err = sched.setupTasksList(tx)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should correctly initialize in-use volumes", func() {
			Expect(sched.volumes).ToNot(BeNil())
			vs := sched.volumes
			Expect(vs.volumes).To(SatisfyAll(
				HaveKey("volumeID1"),
				HaveKey("volumeID2"),
				HaveKey("volumeID3"),
			))

			Expect(vs.volumes["volumeID1"].tasks).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("runningTask", volumeUsage{nodeID: "nodeID0"}),
			))

			Expect(vs.volumes["volumeID2"].tasks).To(BeEmpty())

			Expect(vs.volumes["volumeID3"].tasks).To(SatisfyAll(
				HaveLen(1),
				HaveKeyWithValue("runningTask", volumeUsage{nodeID: "nodeID0"}),
			))
		})
	})
})
