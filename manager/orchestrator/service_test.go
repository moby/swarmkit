package orchestrator

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/assert"
)

// TestIsReplicatedJob tests that IsReplicatedJob only returns true when the
// service mod is ReplicatedJob
func TestIsReplicatedJob(t *testing.T) {
	// first, create a spec with no mode, that we can reuse for each subtest.
	service := &api.Service{
		ID:   "someService",
		Spec: api.ServiceSpec{},
	}
	// this might seem like a good use-case for a table-based test, but the
	// various service modes do not share a common public interface, and so
	// cannot be easily assigned to the same type

	service.Spec.Mode = &api.ServiceSpec_ReplicatedJob{}
	assert.True(t, IsReplicatedJob(service))

	service.Spec.Mode = &api.ServiceSpec_GlobalJob{}
	assert.False(t, IsReplicatedJob(service))

	service.Spec.Mode = &api.ServiceSpec_Replicated{}
	assert.False(t, IsReplicatedJob(service))

	service.Spec.Mode = &api.ServiceSpec_Global{}
	assert.False(t, IsReplicatedJob(service))
}

// TestIsGlobalJob tests that IsGlobalJob only returns true when the
// service mod is GlobalJob. This test is pretty much identical to
// TestIsReplicatedJob. There is probably a clever DRY solution to encompass
// both functions in one test, but these are really braindead simple tests so
// it's just easier to cut and paste.
func TestIsGlobalJob(t *testing.T) {
	// first, create a spec with no mode, that we can reuse for each subtest.
	service := &api.Service{
		ID:   "someService",
		Spec: api.ServiceSpec{},
	}
	// this might seem like a good use-case for a table-based test, but the
	// various service modes do not share a common public interface, and so
	// cannot be easily assigned to the same type

	service.Spec.Mode = &api.ServiceSpec_ReplicatedJob{}
	assert.False(t, IsGlobalJob(service))

	service.Spec.Mode = &api.ServiceSpec_GlobalJob{}
	assert.True(t, IsGlobalJob(service))

	service.Spec.Mode = &api.ServiceSpec_Replicated{}
	assert.False(t, IsGlobalJob(service))

	service.Spec.Mode = &api.ServiceSpec_Global{}
	assert.False(t, IsGlobalJob(service))
}
