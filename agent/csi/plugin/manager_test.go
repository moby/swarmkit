package plugin

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/docker/swarmkit/api"
)

var _ = Describe("PluginManager", func() {
	var (
		pm *pluginManager
	)

	BeforeEach(func() {
		pm = &pluginManager{
			plugins:           map[string]NodePlugin{},
			newNodePluginFunc: newFakeNodePlugin,
		}

		pm.plugins["plug1"] = newFakeNodePlugin("plug1", "", nil)
		pm.plugins["plug2"] = newFakeNodePlugin("plug2", "fail", nil)
		pm.plugins["plug3"] = newFakeNodePlugin("plug3", "", nil)
	})

	Describe("Get", func() {
		It("should return the requested plugin", func() {
			p, err := pm.Get("plug1")
			Expect(err).ToNot(HaveOccurred())
			Expect(p).ToNot(BeNil())
		})

		It("should return an error if no plugin can be found", func() {
			p, err := pm.Get("plugNotHere")
			Expect(err).To(HaveOccurred())
			Expect(p).To(BeNil())
		})
	})

	Describe("Set", func() {
		var (
			plug1Before, plug3Before NodePlugin
		)

		BeforeEach(func() {
			plugins := []*api.CSINodePlugin{
				{
					Name: "plug1",
				}, {
					Name:   "plug3",
					Socket: "fail",
				}, {
					Name: "plug4",
				},
			}

			plug1Before = pm.plugins["plug1"]
			plug3Before = pm.plugins["plug3"]

			err := pm.Set(plugins)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should create plugins that do not already exist", func() {
			p, err := pm.Get("plug4")
			Expect(err).ToNot(HaveOccurred())
			Expect(p).ToNot(BeNil())
		})

		It("should remove plugins that exist in the PluginManager, but are not in the arg", func() {
			p, err := pm.Get("plug2")
			Expect(err).To(HaveOccurred())
			Expect(p).To(BeNil())
		})

		It("should not re-create plugins which are already created", func() {
			p1, err := pm.Get("plug1")
			Expect(err).ToNot(HaveOccurred())
			Expect(p1).ToNot(BeNil())
			Expect(p1).To(Equal(plug1Before))

			p3, err := pm.Get("plug3")
			Expect(err).ToNot(HaveOccurred())
			Expect(p3).ToNot(BeNil())
			Expect(p3).To(Equal(plug3Before))
		})
	})

	Describe("NodeInfo", func() {
		It("should return NodeCSIInfo for every active plugin", func() {
			info, err := pm.NodeInfo(context.Background())
			Expect(err).ToNot(HaveOccurred())

			pluginNames := []string{}
			for _, i := range info {
				pluginNames = append(pluginNames, i.PluginName)
			}

			Expect(pluginNames).To(ConsistOf("plug1", "plug3"))
		})
	})
})
