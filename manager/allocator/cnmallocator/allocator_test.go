package cnmallocator

import (
	"testing"

	"github.com/moby/swarmkit/v2/manager/allocator"
)

func TestAllocator(t *testing.T) {
	allocator.RunAllocatorTests(t, NewProvider(nil))
}
