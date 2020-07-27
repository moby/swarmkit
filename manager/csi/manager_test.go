package csi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

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

		// read out all events from the watch channel. in order to ensure that
		// we don't exit before an event is even queued up, wait until we
		// actually see that event
		var updateHappened bool
	eventsLoop:
		for {
			select {
			case ev := <-watch:
				// wait for the cluster update event
				if _, ok := ev.(api.EventUpdateCluster); ok {
					updateHappened = true
				}
				vm.handleEvent(ev)
			default:
				// when no events remain, break out of the loop
				if updateHappened {
					break eventsLoop
				}
			}
		}

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

		updateHappened = false
	eventsLoop2:
		for {
			select {
			case ev := <-watch:
				// wait for the cluster update event
				if _, ok := ev.(api.EventUpdateCluster); ok {
					updateHappened = true
				}
				vm.handleEvent(ev)
			default:
				// when no events remain, break out of the loop
				if updateHappened {
					break eventsLoop2
				}
			}
		}

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

			volumeCreated := false
		eventLoop:
			for {
				select {
				case ev := <-watch:
					if _, ok := ev.(api.EventCreateVolume); ok {
						volumeCreated = true
					}
					vm.handleEvent(ev)
				default:
					if volumeCreated {
						break eventLoop
					}
				}
			}
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

})
