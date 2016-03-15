package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceConfigValidate(t *testing.T) {
	for _, bad := range []*ServiceConfig{
		{Name: "", ContainerConfig: ContainerConfig{Image: ""}},
		{Name: "name", ContainerConfig: ContainerConfig{Image: ""}},
		{Name: "", ContainerConfig: ContainerConfig{Image: "image"}},
	} {
		assert.Error(t, bad.Validate())
	}

	for _, good := range []ServiceConfig{
		{Name: "name", ContainerConfig: ContainerConfig{Image: "image"}},
	} {

		assert.NoError(t, good.Validate())
	}
}

func TestDiffServiceConfigs(t *testing.T) {
	service := &ServiceConfig{Name: "name", Instances: 1, ContainerConfig: ContainerConfig{Image: "nginx"}}

	diff, err := service.Diff(
		&ServiceConfig{Name: "name", Instances: 1, ContainerConfig: ContainerConfig{Image: "redis"}},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- remote\n+++ local\n@@ -1 +1 @@\n-image: redis\n+image: nginx\n", diff)

	diff, err = service.Diff(
		&ServiceConfig{Name: "name", Instances: 2, ContainerConfig: ContainerConfig{Image: "nginx"}},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- remote\n+++ local\n@@ -3 +3 @@\n-instances: 2\n+instances: 1\n", diff)
}
