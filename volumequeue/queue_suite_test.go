package volumequeue

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVolumesQueue(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VolumeQueue Suite")
}
