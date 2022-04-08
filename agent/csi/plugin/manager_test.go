package plugin

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/moby/swarmkit/v2/testutils"
)

var _ = Describe("PluginManager", func() {
	var (
		pm *pluginManager
		pg *testutils.FakePluginGetter
	)

	BeforeEach(func() {
		pg = &testutils.FakePluginGetter{
			Plugins: map[string]*testutils.FakeCompatPlugin{},
		}

		pm = &pluginManager{
			plugins:           map[string]NodePlugin{},
			newNodePluginFunc: newFakeNodePlugin,
			pg:                pg,
		}

		pg.Plugins["plug1"] = &testutils.FakeCompatPlugin{
			PluginName: "plug1",
			PluginAddr: &net.UnixAddr{
				Net:  "unix",
				Name: "",
			},
		}
		pg.Plugins["plug2"] = &testutils.FakeCompatPlugin{
			PluginName: "plug2",
			PluginAddr: &net.UnixAddr{
				Net:  "unix",
				Name: "fail",
			},
		}
		pg.Plugins["plug3"] = &testutils.FakeCompatPlugin{
			PluginName: "plug3",
			PluginAddr: &net.UnixAddr{
				Net:  "unix",
				Name: "",
			},
		}
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
