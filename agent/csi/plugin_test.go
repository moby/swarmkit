package csi

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"context"

	"github.com/docker/swarmkit/api"
)

const driverName = "testdriver"

var _ = Describe("Plugin Node", func() {
	var (
		nodeClient *NodePlugin

		nodeInfo *api.NodeCSIInfo

		err error
	)

	BeforeEach(func() {

		nodeClient = NewFakeNodePlugin("plugin-11", "nodeid1", true)

	})

	JustBeforeEach(func() {
		nodeInfo, err = nodeClient.NodeGetInfo(context.Background())
	})
	It("should return a correct NodeGetInfoResponse object", func() {
		Expect(nodeInfo).ToNot(BeNil())
		Expect(nodeInfo.NodeID).To(Equal("nodeid1"))
	})
	It("should not return an error", func() {
		Expect(err).ToNot(HaveOccurred())
	})

})
