package plugin

import (
	"context"
	"net"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("PluginManager", func() {
	var (
		pm *pluginManager
		pg *fakePluginGetter
	)

	BeforeEach(func() {
		pg = &fakePluginGetter{
			plugins: map[string]*fakeCompatPlugin{},
		}

		pm = &pluginManager{
			plugins:           map[string]NodePlugin{},
			newNodePluginFunc: newFakeNodePlugin,
			pg:                pg,
		}

		pg.plugins["plug1"] = &fakeCompatPlugin{
			name: "plug1",
			addr: &net.UnixAddr{
				Net:  "unix",
				Name: "",
			},
		}
		pg.plugins["plug2"] = &fakeCompatPlugin{
			name: "plug2",
			addr: &net.UnixAddr{
				Net:  "unix",
				Name: "fail",
			},
		}
		pg.plugins["plug3"] = &fakeCompatPlugin{
			name: "plug3",
			addr: &net.UnixAddr{
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
