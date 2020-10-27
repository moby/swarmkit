package csi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"

	"github.com/container-storage-interface/spec/lib/go/csi"
	"github.com/docker/swarmkit/api"
)

const driverName = "testdriver"

// newPluginFromClients creates a new node plugin using the provided CSI RPC
// clients.
func newPluginFromClients(name string, nodeClient csi.NodeClient, nodeId string) *NodePlugin {
	return &NodePlugin{
		name:       name,
		nodeClient: nodeClient,
		nodeID:     nodeId,
	}
}

var _ = Describe("Plugin Node", func() {
	var (
		nodePlugin *NodePlugin

		nodeClient *fakeNodeClient

		nodeInfo *api.NodeCSIInfo

		err error
	)

	BeforeEach(func() {

		nodeClient = newFakeNodeClient()

		nodePlugin = newPluginFromClients(driverName, nodeClient, "1")
	})

	JustBeforeEach(func() {
		nodeInfo, err = nodePlugin.NodeGetInfo(context.Background())
	})
	It("should return a correct NodeCSIInfo object", func() {
		Expect(nodeInfo).ToNot(BeNil())
		Expect(nodeInfo.NodeID).To(Equal("nodeid1"))
	})
	It("should not return an error", func() {
		Expect(err).ToNot(HaveOccurred())
	})
	It("should create correct node info requests", func() {
		Expect(nodeClient.NodeGetInfo).To(HaveLen(1))
	})

})
