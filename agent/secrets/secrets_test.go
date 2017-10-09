package secrets

import (
	"github.com/docker/swarmkit/api"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestRestrict(t *testing.T) {
	const (
		containerSecret1 = "secret1"
		containerSecret2 = "secret2"
		genericSecret3   = "secret3"
		taskReference    = "task"
		configReference  = "config"
	)

	task := &api.Task{
		Spec: api.TaskSpec{
			Runtime: &api.TaskSpec_Container{
				Container: &api.ContainerSpec{
					Secrets: []*api.SecretReference{
						{SecretID: containerSecret1},
						{SecretID: containerSecret2},
					},
				},
			},
			ResourceReferences: []api.ResourceReference{
				{ResourceID: genericSecret3, ResourceType: api.ResourceType_SECRET},
				{ResourceID: taskReference, ResourceType: api.ResourceType_TASK},
				{ResourceID: configReference, ResourceType: api.ResourceType_CONFIG},
			},
		},
	}
	ids := Restrict(NewManager(), task).(*taskRestrictedSecretsProvider).secretIDs
	assert.Len(t, ids, 3, "3 secrets are added")
	assert.NotNil(t, ids[containerSecret1])
	assert.NotNil(t, ids[containerSecret2])
	assert.NotNil(t, ids[genericSecret3])
}
