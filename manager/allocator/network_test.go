package allocator

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/assert"
)

func TestUpdatePortsInHostPublishMode(t *testing.T) {
	service := api.Service{
		Spec: api.ServiceSpec{
			Endpoint: &api.EndpointSpec{
				Ports: []*api.PortConfig{
					{
						Protocol:      api.ProtocolTCP,
						TargetPort:    80,
						PublishedPort: 10000,
						PublishMode:   api.PublishModeHost,
					},
				},
			},
		},
		Endpoint: &api.Endpoint{
			Ports: []*api.PortConfig{
				{
					Protocol:      api.ProtocolTCP,
					TargetPort:    80,
					PublishedPort: 15000,
					PublishMode:   api.PublishModeHost,
				},
			},
		},
	}
	updatePortsInHostPublishMode(&service)

	assert.Len(t, service.Endpoint.Ports, 1)
	assert.Equal(t, uint32(10000), service.Endpoint.Ports[0].PublishedPort)
	assert.Equal(t, uint32(10000), service.Endpoint.Spec.Ports[0].PublishedPort)
}
