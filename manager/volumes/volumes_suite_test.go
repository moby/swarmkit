package volumes

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"github.com/sirupsen/logrus"
)

func TestVolumes(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Volumes Suite")
}

var _ = BeforeSuite(func() {
	logrus.SetOutput(GinkgoWriter)
})
