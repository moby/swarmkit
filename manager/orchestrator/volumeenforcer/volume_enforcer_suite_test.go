package volumeenforcer

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestVolumeEnforcer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "VolumeEnforcer Suite")
}
