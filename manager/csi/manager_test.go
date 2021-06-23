package csi

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/types"

	"github.com/docker/go-events"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/state/store"
)

var _ = Describe("Manager", func() {
	// The Manager unit tests are intended to mainly avoid using a
	// goroutine with the Manager by calling init and handleEvent
	// directly, instead of executing the run loop.
	var (
		vm *Manager
		s  *store.MemoryStore

		// nodes is a slice of all nodes to create during setup
		nodes []*api.Node

		// volumes is a slice of all volumes to create during setup
		volumes []*api.Volume

		pluginMaker  *fakePluginMaker
		pluginGetter *fakePluginGetter

		// watch contains an event channel, which is produced by
		// store.ViewAndWatch.
		watch chan events.Event
		// watchCancel cancels the watch operation.
		watchCancel func()

		ctx context.Context
	)

	// passEvents passes all events from the watch channel to the handleEvent
	// method, checking each one against match, until match returns true.
	// when match is true, processVolumes is called to process all outstanding
	// volume operations, and then the function returns.
	//
	// passEvents allows us to do an end-run around the Run method of the
	// volume manager, letting us turn a typically asynchronous operation into
	// a synchronous one for the purpose of testing.
	passEvents := func(match func(events.Event) bool) {
		for ev := range watch {
			vm.handleEvent(ev)
			if match(ev) {
				return
			}
		}
	}

	BeforeEach(func() {
		ctx = context.Background()
		pluginMaker = &fakePluginMaker{
			plugins: map[string]*fakePlugin{},
		}
		pluginGetter = &fakePluginGetter{
			plugins: map[string]*fakeCompatPlugin{},
		}

		s = store.NewMemoryStore(nil)

		nodes = []*api.Node{}
		volumes = []*api.Volume{}

		vm = NewManager(s, pluginGetter)
		vm.newPlugin = pluginMaker.newFakePlugin
	})

	JustBeforeEach(func() {
		err := s.Update(func(tx store.Tx) error {
			for _, node := range nodes {
				if err := store.CreateNode(tx, node); err != nil {
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

		// start the watch after everything else is set up, so we don't get any
		// events from our setup phase.
		watch, watchCancel, err = store.ViewAndWatch(s, func(tx store.ReadTx) error {
			// TODO(dperny): use the regular Watch, not ViewAndWatch
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
			pluginGetter.plugins["newPlugin"] = &fakeCompatPlugin{
				name: "newPlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///whatever.sock",
				},
			}
			pluginGetter.plugins["differentPlugin"] = &fakeCompatPlugin{
				name: "differentPlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///somethingElse.sock",
				},
			}

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

			volumes = append(volumes,
				&api.Volume{
					ID: "volumeID1",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume1",
						},
						Driver: &api.Driver{
							Name: "newPlugin",
						},
					},
				},
				&api.Volume{
					ID: "volumeID2",
					Spec: api.VolumeSpec{
						Annotations: api.Annotations{
							Name: "volume2",
						},
						Driver: &api.Driver{
							Name: "newPlugin",
						},
					},
					VolumeInfo: &api.VolumeInfo{
						VolumeID: "volumePluginID",
					},
				},
			)
		})

		JustBeforeEach(func() {
			// we can use context.Background because the context here is only
			// used for logging.
			vm.init(context.Background())
		})

		It("should create all Plugins", func() {
			// NOTE(dperny): originally, plugins were eagerly loaded, and this
			// check ensured that. Now, plugins are lazy-loaded, but this check
			// is still valid because lazy-loaded plugins will be loaded when
			// the node is added.
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

		It("should enqueue all volumes", func() {
			Expect(vm.pendingVolumes.Outstanding()).To(Equal(2))
		})
	})

	When("a volume is created", func() {
		BeforeEach(func() {
			pluginGetter.plugins["somePlugin"] = &fakeCompatPlugin{
				name: "somePlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///whatever.sock",
				},
			}
			pluginGetter.plugins["someOtherPlugin"] = &fakeCompatPlugin{
				name: "someOtherPlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///somethingElse.sock",
				},
			}
		})

		JustBeforeEach(func() {
			vm.init(context.Background())
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

			vm.processVolume(ctx, volume.ID, 0)
		})

		It("should call the correct plugin to create volumes", func() {
			Expect(pluginMaker.plugins["somePlugin"].volumesCreated).To(HaveKey("someVolume"))
			Expect(pluginMaker.plugins["someOtherPlugin"]).To(BeNil())
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

		It("should not requeue a successful volume", func() {
			Expect(vm.pendingVolumes.Outstanding()).To(Equal(0))
		})
	})

	Describe("managing node inventory", func() {
		BeforeEach(func() {
			pluginGetter.plugins["newPlugin"] = &fakeCompatPlugin{
				name: "newPlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///whatever.sock",
				},
			}
			pluginGetter.plugins["differentPlugin"] = &fakeCompatPlugin{
				name: "differentPlugin",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///somethingElse.sock",
				},
			}

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
			vm.init(context.Background())
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

			passEvents(func(ev events.Event) bool {
				e, ok := ev.(api.EventCreateNode)
				return ok && e.Node.ID == "nodeID3"
			})

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

			passEvents(func(ev events.Event) bool {
				e, ok := ev.(api.EventUpdateNode)
				return ok && e.Node.ID == "nodeIDextra"
			})

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

			passEvents(func(ev events.Event) bool {
				_, ok := ev.(api.EventDeleteNode)
				return ok
			})

			Expect(pluginMaker.plugins["newPlugin"].removedIDs).To(SatisfyAll(
				HaveLen(1), HaveKey("nodeID1"),
			))
			Expect(pluginMaker.plugins["differentPlugin"].removedIDs).To(SatisfyAll(
				HaveLen(1), HaveKey("nodeID1"),
			))
		})
	})

	Describe("publishing and unpublishing volumes", func() {
		var (
			v1 *api.Volume
		)
		BeforeEach(func() {
			pluginGetter.plugins["plug1"] = &fakeCompatPlugin{
				name: "plug1",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///whatever.sock",
				},
			}

			nodes = append(nodes,
				&api.Node{
					ID: "node1",
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "plug1",
								NodeID:     "plug1Node1",
							},
						},
					},
				},
				&api.Node{
					ID: "node2",
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "plug1",
								NodeID:     "plug1Node2",
							},
						},
					},
				},
				&api.Node{
					ID: "node3",
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "plug1",
								NodeID:     "plug1Node3",
							},
						},
					},
				},
				&api.Node{
					ID: "node4",
					Description: &api.NodeDescription{
						CSIInfo: []*api.NodeCSIInfo{
							{
								PluginName: "plug1",
								NodeID:     "plug1Node4",
							},
						},
					},
				},
			)

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
				PublishStatus: []*api.VolumePublishStatus{
					{
						NodeID: "node1",
						State:  api.VolumePublishStatus_PENDING_PUBLISH,
					}, {
						NodeID:         "node3",
						PublishContext: map[string]string{"unpublish": "thisone"},
						State:          api.VolumePublishStatus_PENDING_UNPUBLISH,
					}, {
						NodeID: "node2",
						State:  api.VolumePublishStatus_PENDING_PUBLISH,
					}, {
						NodeID:         "node4",
						PublishContext: map[string]string{"unpublish": "thisone"},
						State:          api.VolumePublishStatus_PENDING_UNPUBLISH,
					},
				},
			}
		})

		JustBeforeEach(func() {
			vm.init(context.Background())

			// do the creation after the initialization, so that the init does
			// not enqueue the volumes for processing.
			err := s.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, v1)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should call ControllerPublishVolume for each pending PublishStatus", func() {
			vm.processVolume(ctx, v1.ID, 0)

			// node1 and node2 should be published, and node3 and node4 should
			// be deleted
			haveBeenPublished := func() GomegaMatcher {
				return WithTransform(
					func(v *api.Volume) []*api.VolumePublishStatus {
						if v == nil {
							return nil
						}
						return v.PublishStatus
					},
					ConsistOf(
						&api.VolumePublishStatus{
							NodeID:         "node1",
							State:          api.VolumePublishStatus_PUBLISHED,
							PublishContext: map[string]string{"faked": "yeah"},
						},
						&api.VolumePublishStatus{
							NodeID:         "node2",
							State:          api.VolumePublishStatus_PUBLISHED,
							PublishContext: map[string]string{"faked": "yeah"},
						},
					),
				)
			}

			s.View(func(tx store.ReadTx) {
				v1 = store.GetVolume(tx, v1.ID)
			})
			Expect(v1).To(haveBeenPublished())

			// verify, additionally, that ControllerPublishVolume has actually
			// been called
			Expect(pluginMaker.plugins["plug1"].volumesPublished[v1.ID]).To(
				ConsistOf("node1", "node2"),
			)
			Expect(pluginMaker.plugins["plug1"].volumesUnpublished[v1.ID]).To(
				ConsistOf("node3", "node4"),
			)

			Expect(vm.pendingVolumes.Outstanding()).To(Equal(0))
		})

		It("should fail gracefully and only in part", func() {
			v1.Spec.Annotations.Labels = map[string]string{
				failPublishLabel: "node2,node4",
			}
			err := s.Update(func(tx store.Tx) error {
				return store.UpdateVolume(tx, v1)
			})
			Expect(err).ToNot(HaveOccurred())

			vm.processVolume(ctx, v1.ID, 0)

			By("still updating and committing the volume to the store")
			var (
				updatedVolume                         *api.Volume
				nodeStatus1, nodeStatus2, nodeStatus4 *api.VolumePublishStatus
			)

			s.View(func(tx store.ReadTx) {
				updatedVolume = store.GetVolume(tx, v1.ID)
			})
			Expect(updatedVolume).ToNot(BeNil())
			Expect(updatedVolume.PublishStatus).To(HaveLen(3))

			for _, status := range updatedVolume.PublishStatus {
				switch status.NodeID {
				case "node1":
					nodeStatus1 = status
				case "node2":
					nodeStatus2 = status
				case "node4":
					nodeStatus4 = status
				}
			}

			By("updating any PublishStatuses that succeed")
			Expect(nodeStatus1.State).To(Equal(api.VolumePublishStatus_PUBLISHED))
			Expect(nodeStatus1.Message).To(BeEmpty())

			By("explaining the cause for the failure in the status message")
			Expect(nodeStatus2.State).To(Equal(api.VolumePublishStatus_PENDING_PUBLISH))
			Expect(nodeStatus2.Message).ToNot(BeEmpty())

			Expect(nodeStatus4.State).To(Equal(api.VolumePublishStatus_PENDING_UNPUBLISH))
			Expect(nodeStatus4.Message).ToNot(BeEmpty())

			By("enqueuing the volume for a retry")
			Expect(vm.pendingVolumes.Outstanding()).To(Equal(1))
		})
	})

	Describe("removing a Volume", func() {
		BeforeEach(func() {
			pluginGetter.plugins["plug"] = &fakeCompatPlugin{
				name: "plug",
				addr: &net.UnixAddr{
					Net:  "unix",
					Name: "unix:///whatever.sock",
				},
			}
		})

		JustBeforeEach(func() {
			vm.init(context.Background())

			// do creation after initialization to avoid init enqueuing the
			// volume
			volume := &api.Volume{
				ID: "volumeID",
				Spec: api.VolumeSpec{
					Annotations: api.Annotations{
						Name: "volumeName",
					},
					Driver: &api.Driver{
						Name: "plug",
					},
					Availability: api.VolumeAvailabilityDrain,
				},
				VolumeInfo: &api.VolumeInfo{
					VolumeID: "plugID",
				},
			}

			err := s.Update(func(tx store.Tx) error {
				return store.CreateVolume(tx, volume)
			})
			Expect(err).ToNot(HaveOccurred())
		})

		It("should delete the Volume", func() {
			err := s.Update(func(tx store.Tx) error {
				v := store.GetVolume(tx, "volumeID")
				v.PendingDelete = true
				return store.UpdateVolume(tx, v)
			})
			Expect(err).ToNot(HaveOccurred())
			vm.processVolume(ctx, "volumeID", 0)

			By("calling DeleteVolume on the plugin")
			Expect(pluginMaker.plugins["plug"].volumesDeleted).To(ConsistOf("volumeID"))

			By("removing the Volume from the store")
			var v *api.Volume
			s.View(func(tx store.ReadTx) {
				v = store.GetVolume(tx, "volumeID")
			})
			Expect(v).To(BeNil())

			// check that pendingVolumes is empty, which will not be the case
			// if the delete operation failed.
			Expect(vm.pendingVolumes.Outstanding()).To(Equal(0))
		})

		It("should not remove the volume from the store if DeleteVolume fails", func() {
			err := s.Update(func(tx store.Tx) error {
				v := store.GetVolume(tx, "volumeID")
				v.PendingDelete = true
				v.Spec.Annotations.Labels = map[string]string{
					failDeleteLabel: "failing delete test",
				}
				return store.UpdateVolume(tx, v)
			})
			Expect(err).ToNot(HaveOccurred())

			vm.processVolume(ctx, "volumeID", 0)

			Expect(pluginMaker.plugins["plug"].volumesDeleted).To(ConsistOf("volumeID"))

			Consistently(func() *api.Volume {
				var v *api.Volume
				s.View(func(tx store.ReadTx) {
					v = store.GetVolume(tx, "volumeID")
				})
				return v
			}).ShouldNot(BeNil())

			// this will create a timer, but because it's only the first retry,
			// it is a very short timer, and in any case, the entry remains in
			// outstanding until it is plucked off by a call to wait.
			Expect(vm.pendingVolumes.Outstanding()).To(Equal(1))
		})
	})
})
