package csi

import (
	"testing"

	"github.com/moby/swarmkit/v2/log"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVolumes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CSI Volumes Suite")
}

var _ = BeforeSuite(func() {
	log.L.Logger.SetOutput(GinkgoWriter)
})
