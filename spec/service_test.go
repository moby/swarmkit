package spec

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestServiceConfigValidate(t *testing.T) {
	bad := []*ServiceConfig{
		// Missing name and image.
		{
			Name: "",
			ContainerConfig: ContainerConfig{
				Image: "",
			},
		},

		// Missing image
		{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "",
			},
		},

		// Missing name
		{
			Name: "",
			ContainerConfig: ContainerConfig{
				Image: "image",
			},
		},

		// incorrect service mode
		{
			Name: "name",
			Mode: "invalid",
			ContainerConfig: ContainerConfig{
				Image: "image",
			},
		},

		// Invalid memory limit
		{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "image",
				Resources: &ResourceRequirements{
					Limits: &Resources{
						Memory: "invalid",
					},
				},
			},
		},
	}

	good := []*ServiceConfig{
		{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "image",
			},
		},

		{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "image",
				Resources: &ResourceRequirements{
					Limits: &Resources{
						Memory: "1024",
					},
				},
			},
		},

		// test service mode
		{
			Name: "name",
			Mode: "fill",
			ContainerConfig: ContainerConfig{
				Image: "image",
			},
		},

		{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "image",
				Resources: &ResourceRequirements{
					Limits: &Resources{
						Memory: "1KiB",
					},
				},
			},
		},
	}

	for _, i := range bad {
		assert.Error(t, i.Validate())
	}

	for _, i := range good {
		assert.NoError(t, i.Validate())
	}
}

func TestServiceConfigsDiff(t *testing.T) {
	makeService := func() *ServiceConfig {
		s := &ServiceConfig{
			Name: "name",
			ContainerConfig: ContainerConfig{
				Image: "nginx",

				Resources: &ResourceRequirements{
					Limits: &Resources{
						Memory: "1GiB",
					},
				},
			},
		}
		assert.NoError(t, s.Validate())
		return s
	}
	service := makeService()

	against := makeService()
	diff, err := service.Diff(0, "remote", "local", against)
	assert.NoError(t, err)
	assert.Empty(t, diff)

	against = &ServiceConfig{}
	against.FromProto(service.ToProto())
	diff, err = service.Diff(0, "remote", "local", against)
	assert.NoError(t, err)
	assert.Empty(t, diff)

	against = makeService()
	against.Image = "redis"
	assert.NoError(t, against.Validate())
	diff, err = service.Diff(0, "remote", "local", against)
	assert.NoError(t, err)
	assert.Contains(t, diff, "image: nginx")

	against = makeService()
	instances := int64(2)
	against.Instances = &instances
	assert.NoError(t, against.Validate())
	diff, err = service.Diff(0, "old", "new", against)
	assert.NoError(t, err)
	assert.Contains(t, diff, "instances: 2")

	against = makeService()
	against.Env = []string{"DEBUG=1"}
	assert.NoError(t, against.Validate())
	diff, err = service.Diff(0, "old", "new", against)
	assert.NoError(t, err)
	assert.Contains(t, diff, "DEBUG=1")

	against = makeService()
	against.Resources.Limits.Memory = "2Gi"
	assert.NoError(t, against.Validate())
	diff, err = service.Diff(0, "old", "new", against)
	assert.NoError(t, err)
	// 2GB = 2147483648
	assert.Contains(t, diff, "memory: 2.0 GiB")

	against = makeService()
	// 1GB and 1024MB shouldn't trigger a diff.
	against.Resources.Limits.Memory = "1024MiB"
	assert.NoError(t, against.Validate())
	diff, err = service.Diff(0, "old", "new", against)
	assert.NoError(t, err)
	assert.Empty(t, diff)
}
