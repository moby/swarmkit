package csi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/types"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
)

// passEvents calls handle and then match with each event from the evs channel
// until match returns true. this allows passing events until a specific
// desired event has been passed.
func passEvents(evs <-chan events.Event, match func(events.Event) bool, handle func(events.Event)) {
	for ev := range evs {
		handle(ev)
		if match(ev) {
			return
		}
	}
}

var _ = Describe("Manager", func() {
	// The Manager unit tests are intended to mainly avoid using a
	// goroutine with the Manager by calling init and handleEvent
	// directly, instead of executing the run loop.
	var (
		vm *Manager
		s  *store.MemoryStore

		cluster *api.Cluster

		// plugins is a slice of all plugins used in a particular test.
		plugins []*api.CSIConfig_Plugin

		// nodes is a slice of all nodes to create during setup
		nodes []*api.Node

		pluginMaker *fakePluginMaker

		// watch contains an event channel, which is produced by
		// store.ViewAndWatch.
		watch chan events.Event
		// watchCancel cancels the watch operation.
		watchCancel func()
	)

	BeforeEach(func() {
		pluginMaker = &fakePluginMaker{
			plugins: map[string]*fakePlugin{},
		}

		s = store.NewMemoryStore(nil)

		plugins = []*api.CSIConfig_Plugin{}
		nodes = []*api.Node{}

		vm = NewManager(s)
		vm.newPlugin = pluginMaker.newFakePlugin
	})

	JustBeforeEach(func() {
		cluster = &api.Cluster{
			ID: "somecluster",
			Spec: api.ClusterSpec{
				Annotations: api.Annotations{
					Name: store.DefaultClusterName,
				},
				CSIConfig: api.CSIConfig{
					Plugins: plugins,
				},
			},
		}

		err := s.Update(func(tx store.Tx) error {
			for _, node := range nodes {
				if err := store.CreateNode(tx, node); err != nil {
					return err
				}
			}
			return store.CreateCluster(tx, cluster)
		})

		Expect(err).ToNot(HaveOccurred())

		// start the watch after everything else is set up, so we don't get any
		// events from our setup phase.
		watch, watchCancel, err = store.ViewAndWatch(s, func(tx store.ReadTx) error {
			// because setting up the cluster object is done as part of the run
			// function, not the init function, we must do that work here
			// manually.
			clusters, _ := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
			vm.cluster = clusters[0]
			return nil
		})
		Expect(err).ToNot(HaveOccurred())
	})

	AfterEach(func() {
		// always cancel the watch, to avoid leaking. I think.
		watchCancel()
	})

	When("starting up", func() {
		BeforeEach(func() {
			plugins = append(plugins,
				&api.CSIConfig_Plugin{
					Name:   "newPlugin",
					Socket: "unix:///whatever.sock",
				},
				&api.CSIConfig_Plugin{
					Name:   "differentPlugin",
					Socket: "unix:///somethingElse.sock",
				},
			)

			nodes = append(nodes,
				&api.Node{
					ID: "nodeID1",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "node1",
						},
					},
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeID:     "newPluginNode1",
							}, {
								PluginName: "differentPlugin",
								NodeID:     "differentPluginNode1",
							},
						},
					},
				},
				&api.Node{
					ID: "nodeID2",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "node2",
						},
					},
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeID:     "newPluginNode2",
							}, {
								PluginName: "differentPlugin",
								NodeID:     "differentPluginNode2",
							},
						},
					},
				},
			)
		})

		JustBeforeEach(func() {
			vm.init()
		})

		It("should create all Plugins", func() {
			Expect(vm.plugins).To(HaveLen(2))
			Expect(pluginMaker.plugins).To(SatisfyAll(
				HaveLen(2), HaveKey("newPlugin"), HaveKey("differentPlugin"),
			))
		})

		It("should add all nodes to the appropriate plugins", func() {
			newPlugin := pluginMaker.plugins["newPlugin"]
			Expect(newPlugin.swarmToCSI).To(SatisfyAll(
				HaveLen(2),
				HaveKeyWithValue("nodeID1", "newPluginNode1"),
				HaveKeyWithValue("nodeID2", "newPluginNode2"),
			))

			differentPlugin := pluginMaker.plugins["differentPlugin"]
			Expect(differentPlugin.swarmToCSI).To(SatisfyAll(
				HaveLen(2),
				HaveKeyWithValue("nodeID1", "differentPluginNode1"),
				HaveKeyWithValue("nodeID2", "differentPluginNode2"),
			))
		})
	})

	It("should add and remove plugins when the cluster is updated", func() {
		err := s.Update(func(tx store.Tx) error {
			clusters, _ := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
			c := clusters[0]
			c.Spec.CSIConfig.Plugins = append(c.Spec.CSIConfig.Plugins,
				&api.CSIConfig_Plugin{
					Name:   "newPlugin",
					Socket: "whatever",
				},
				&api.CSIConfig_Plugin{
					Name:   "newPlugin2",
					Socket: "whateverElse",
				},
			)
			return store.UpdateCluster(tx, c)
		})
		Expect(err).ToNot(HaveOccurred())

		passEvents(watch, func(ev events.Event) bool {
			_, ok := ev.(api.EventUpdateCluster)
			return ok
		}, vm.handleEvent)

		Expect(pluginMaker.plugins).To(SatisfyAll(
			HaveLen(2), HaveKey("newPlugin"), HaveKey("newPlugin2"),
		))

		Expect(vm.plugins).To(HaveLen(2))

		// now, update again. delete newPlugin, and add newPlugin3
		err = s.Update(func(tx store.Tx) error {
			clusters, _ := store.FindClusters(tx, store.ByName(store.DefaultClusterName))
			c := clusters[0]
			c.Spec.CSIConfig.Plugins = append(
				c.Spec.CSIConfig.Plugins[1:],
				&api.CSIConfig_Plugin{Name: "newPlugin3", Socket: "whateverElseAgain"},
			)
			return store.UpdateCluster(tx, c)
		})
		Expect(err).ToNot(HaveOccurred())

		passEvents(watch, func(ev events.Event) bool {
			_, ok := ev.(api.EventUpdateCluster)
			return ok
		}, vm.handleEvent)

		Expect(vm.plugins).To(SatisfyAll(
			HaveLen(2),
			HaveKey("newPlugin2"),
			HaveKey("newPlugin3"),
		))
		Expect(pluginMaker.plugins).To(HaveKey("newPlugin3"))
	})

	When("a volume is created", func() {
		BeforeEach(func() {
			plugins = append(plugins,
				&api.CSIConfig_Plugin{
					Name:   "somePlugin",
					Socket: "whatever",
				},
				&api.CSIConfig_Plugin{
					Name:   "someOtherPlugin",
					Socket: "whateverElse",
				},
			)
		})

		JustBeforeEach(func() {
			vm.init()
			volume := &api.Volume{
				ID: "someVolume",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volumeName",
					},
					Driver: &api.Driver{
						Name: "somePlugin",
					},
				},
			}

			err := s.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, volume)
			})
			Expect(err).ToNot(HaveOccurred())

			passEvents(watch, func(ev events.Event) bool {
				_, ok := ev.(api.EventCreateVolume)
				return ok
			}, vm.handleEvent)
		})

		It("should call the correct plugin to create volumes", func() {
			Expect(pluginMaker.plugins["somePlugin"].volumesCreated).To(HaveKey("someVolume"))
			Expect(pluginMaker.plugins["someOtherPlugin"].volumesCreated).To(BeEmpty())
		})

		It("should persist the volume in the store", func() {
			var v *api.Volume
			s.View(func(tx store.ReadTx) {
				v = store.GetVolume(tx, "someVolume")
			})

			Expect(v).ToNot(BeNil())
			Expect(v.VolumeInfo).ToNot(BeNil())
			Expect(v.VolumeInfo.VolumeID).To(Equal("csi_someVolume"))
			Expect(v.VolumeInfo.VolumeContext).To(Equal(
				map[string]string{"exists": "yes"},
			))
		})
	})

	Describe("managing node inventory", func() {
		BeforeEach(func() {
			plugins = append(plugins,
				&api.CSIConfig_Plugin{
					Name:   "newPlugin",
					Socket: "unix:///whatever.sock",
				},
				&api.CSIConfig_Plugin{
					Name:   "differentPlugin",
					Socket: "unix:///somethingElse.sock",
				},
			)

			nodes = append(nodes,
				&api.Node{
					ID: "nodeID1",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "node1",
						},
					},
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeID:     "newPluginNode1",
							}, {
								PluginName: "differentPlugin",
								NodeID:     "differentPluginNode1",
							},
						},
					},
				},
				&api.Node{
					ID: "nodeID2",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "node2",
						},
					},
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeID:     "newPluginNode2",
							}, {
								PluginName: "differentPlugin",
								NodeID:     "differentPluginNode2",
							},
						},
					},
				},
				&api.Node{
					ID: "nodeIDextra",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "nodeExtra",
						},
					},
				},
			)
		})

		JustBeforeEach(func() {
			vm.init()
		})

		It("should add new nodes to the plugins", func() {
			err := s.Update(func(tx store.Tx) error {
				return store.CreateNode(tx, &api.Node{
					ID: "nodeID3",
					Spec: api.NodeSpec{
						Annotations: api.Annotations{
							Name: "node3",
						},
					},
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "newPlugin",
								NodeID:     "newPluginNode3",
							}, {
								PluginName: "differentPlugin",
								NodeID:     "differentPluginNode3",
							},
						},
					},
				})
			})
			Expect(err).ToNot(HaveOccurred())

			passEvents(watch, func(ev events.Event) bool {
				e, ok := ev.(api.EventCreateNode)
				return ok && e.Node.ID == "nodeID3"
			}, vm.handleEvent)

			Expect(pluginMaker.plugins["newPlugin"].swarmToCSI).To(SatisfyAll(
				HaveLen(3),
				HaveKeyWithValue("nodeID1", "newPluginNode1"),
				HaveKeyWithValue("nodeID2", "newPluginNode2"),
				HaveKeyWithValue("nodeID3", "newPluginNode3"),
			))

			Expect(pluginMaker.plugins["differentPlugin"].swarmToCSI).To(SatisfyAll(
				HaveLen(3),
				HaveKeyWithValue("nodeID1", "differentPluginNode1"),
				HaveKeyWithValue("nodeID2", "differentPluginNode2"),
				HaveKeyWithValue("nodeID3", "differentPluginNode3"),
			))
		})

		It("should handle node updates", func() {
			err := s.Update(func(tx store.Tx) error {
				node := store.GetNode(tx, "nodeIDextra")
				node.Description = &api.NodeDescription{
					CSIInfo: []*api.NodeCSIInfo{
						{
							PluginName: "differentPlugin",
							NodeID:     "differentPluginNodeExtra",
						},
					},
				}
				return store.UpdateNode(tx, node)
			})
			Expect(err).ToNot(HaveOccurred())

			passEvents(watch, func(ev events.Event) bool {
				e, ok := ev.(api.EventUpdateNode)
				return ok && e.Node.ID == "nodeIDextra"
			}, vm.handleEvent)

			Expect(pluginMaker.plugins["newPlugin"].swarmToCSI).To(SatisfyAll(
				HaveLen(2),
				HaveKeyWithValue("nodeID1", "newPluginNode1"),
				HaveKeyWithValue("nodeID2", "newPluginNode2"),
			))

			Expect(pluginMaker.plugins["differentPlugin"].swarmToCSI).To(SatisfyAll(
				HaveLen(3),
				HaveKeyWithValue("nodeID1", "differentPluginNode1"),
				HaveKeyWithValue("nodeID2", "differentPluginNode2"),
				HaveKeyWithValue("nodeIDextra", "differentPluginNodeExtra"),
			))
		})

		It("should remove nodes from the plugins", func() {
			err := s.Update(func(tx store.Tx) error {
				return store.DeleteNode(tx, "nodeID1")
			})
			Expect(err).ToNot(HaveOccurred())

			passEvents(watch, func(ev events.Event) bool {
				_, ok := ev.(api.EventDeleteNode)
				return ok
			}, vm.handleEvent)

			Expect(pluginMaker.plugins["newPlugin"].removedIDs).To(SatisfyAll(
				HaveLen(1), HaveKey("nodeID1"),
			))
			Expect(pluginMaker.plugins["differentPlugin"].removedIDs).To(SatisfyAll(
				HaveLen(1), HaveKey("nodeID1"),
			))
		})
	})

	Describe("publishing volumes", func() {
		var (
			task   *api.Task
			v1, v2 *api.Volume
		)
		BeforeEach(func() {
			plugins = append(plugins,
				&api.CSIConfig_Plugin{
					Name: "plug1",
				},
				&api.CSIConfig_Plugin{
					Name: "plug2",
				},
			)

			nodes = append(nodes, &api.Node{
				ID: "node1",
				Description: &api.NodeDescription{
					CSIInfo: []*api.NodeCSIInfo{
						{
							PluginName: "plug1",
							NodeID:     "plug1Node1",
						},
						{
							PluginName: "plug2",
							NodeID:     "plug2Node1",
						},
					},
				},
			})

			v1 = &api.Volume{
				ID: "volumeID1",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volume1",
					},
					Driver: &api.Driver{
						Name: "plug1",
					},
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeContext: map[string]string{"foo": "bar"},
					VolumeID:      "plug1VolID1",
				},
			}
			v2 = &api.Volume{
				ID: "volumeID2",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volume2",
					},
					Driver: &api.Driver{
						Name: "plug2",
					},
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeContext: map[string]string{"bas": "bat"},
					VolumeID:      "plug2VolID1",
				},
			}

			task = &api.Task{
				ID:     "task1",
				NodeID: "",
				Spec: api.TaskSpec{
					Runtime: &api.TaskSpec_Container{
						Container: &api.ContainerSpec{
							Mounts: []api.Mount{
								{
									Type:   api.MountTypeCSI,
									Source: "volume1",
									Target: "/home",
								}, {
									Type:   api.MountTypeCSI,
									Source: "volume2",
									Target: "/var",
								},
							},
						},
					},
				},
				Status: api.TaskStatus{
					State: api.TaskStatePending,
				},
			}

			err := s.Update(func(tx store.Tx) error {
				if err := store.CreateTask(tx, task); err != nil {
					return err
				}
				if err := store.CreateVolume(tx, v1); err != nil {
					return err
				}
				return store.CreateVolume(tx, v2)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		JustBeforeEach(func() {
			vm.init()
		})

		It("should call ControllerPublishVolume for each pending PublishStatus", func() {
			v1.PublishStatus = append(v1.PublishStatus, &api.VolumePublishStatus{
				NodeID: "node1",
				State:  api.VolumePublishStatus_PENDING_PUBLISH,
			})
			v2.PublishStatus = append(v2.PublishStatus, &api.VolumePublishStatus{
				NodeID: "node1",
				State:  api.VolumePublishStatus_PENDING_PUBLISH,
			})

			err := s.Update(func(tx store.Tx) error {
				if err := store.UpdateVolume(tx, v1); err != nil {
					return err
				}
				return store.UpdateVolume(tx, v2)
			})
			Expect(err).ToNot(HaveOccurred())

			// Now, we pass those updates and we expect that soon afterward,
			// both Volumes should no longer be pending, and should have an
			// appropriate PublishContext
			for i := 0; i < 2; i++ {
				passEvents(watch, func(ev events.Event) bool {
					_, ok := ev.(api.EventUpdateVolume)
					return ok
				}, vm.handleEvent)
			}

			// couple of quick helpers to make subsequent matchers more readable
			pollStore := func(id string) *api.Volume {
				var v *api.Volume
				s.View(func(tx store.ReadTx) {
					v = store.GetVolume(tx, id)
				})
				return v
			}

			// quick one-off matcher composed from WithTransform
			haveBeenPublished := func() GomegaMatcher {
				return WithTransform(
					func(v *api.Volume) []*api.VolumePublishStatus {
						if v == nil {
							return nil
						}
						return v.PublishStatus
					},
					ConsistOf(&api.VolumePublishStatus{
						NodeID:         "node1",
						State:          api.VolumePublishStatus_PUBLISHED,
						PublishContext: map[string]string{"faked": "yeah"},
					}),
				)
			}

			Eventually(pollStore(v1.ID)).Should(haveBeenPublished())
			Eventually(pollStore(v2.ID)).Should(haveBeenPublished())

			// verify, additionally, that ControllerPublishVolume has actually
			// been called
			Expect(pluginMaker.plugins["plug1"].volumesPublished[v1.ID]).To(
				ConsistOf("node1"),
			)

			Expect(pluginMaker.plugins["plug2"].volumesPublished[v2.ID]).To(
				ConsistOf("node1"),
			)
		})
	})
})
