package allocator

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

// TODO(dperny): rename to just "TestAllocator"
func TestNewAllocatorGinkgo(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "New Allocator Suite")
}
