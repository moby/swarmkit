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

func TestServiceConfigsDiff(t *testing.T) {
	service := &ServiceConfig{Name: "name", Instances: 1, ContainerConfig: ContainerConfig{Image: "nginx"}}

	diff, err := service.Diff(0, "remote", "local",
		&ServiceConfig{Name: "name", Instances: 1, ContainerConfig: ContainerConfig{Image: "redis"}},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- remote\n+++ local\n@@ -1 +1 @@\n-image: redis\n+image: nginx\n", diff)

	diff, err = service.Diff(0, "old", "new",
		&ServiceConfig{Name: "name", Instances: 2, ContainerConfig: ContainerConfig{Image: "nginx"}},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- old\n+++ new\n@@ -3 +3 @@\n-instances: 2\n+instances: 1\n", diff)

	diff, err = service.Diff(0, "old", "new",
		&ServiceConfig{Name: "name", Instances: 2, ContainerConfig: ContainerConfig{Image: "nginx", Env: []string{"DEBUG=1"}}},
	)
	assert.NoError(t, err)
	assert.Equal(t, "--- old\n+++ new\n@@ -2,2 +1,0 @@\n-env:\n-- DEBUG=1\n@@ -5 +3 @@\n-instances: 2\n+instances: 1\n", diff)
}
