package container

import (
	"strings"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"golang.org/x/net/context"
)

type executor struct {
	client engineapi.APIClient
}

// NewExecutor returns an executor from the docker client.
func NewExecutor(client engineapi.APIClient) exec.Executor {
	return &executor{
		client: client,
	}
}

// Describe returns the underlying node description from the docker client.
func (e *executor) Describe(ctx context.Context) (*api.NodeDescription, error) {
	info, err := e.client.Info(ctx)
	if err != nil {
		return nil, err
	}

	var plugins []api.PluginDescription
	addPlugins := func(typ string, names []string) {
		for _, name := range names {
			plugins = append(plugins, api.PluginDescription{
				Type: typ,
				Name: name,
			})
		}
	}

	addPlugins("Volume", info.Plugins.Volume)
	// Add builtin driver "overlay" (the only builtin multi-host driver) to
	// the plugin list by default.
	addPlugins("Network", append([]string{"overlay"}, info.Plugins.Network...))
	addPlugins("Authorization", info.Plugins.Authorization)

	// parse []string labels into a map[string]string
	labels := map[string]string{}
	for _, l := range info.Labels {
		stringSlice := strings.SplitN(l, "=", 2)
		// this will take the last value in the list for a given key
		// ideally, one shouldn't assign multiple values to the same key
		if len(stringSlice) > 1 {
			labels[stringSlice[0]] = stringSlice[1]
		}
	}

	description := &api.NodeDescription{
		Hostname: info.Name,
		Platform: &api.Platform{
			Architecture: info.Architecture,
			OS:           info.OSType,
		},
		Engine: &api.EngineDescription{
			EngineVersion: info.ServerVersion,
			Labels:        labels,
			Plugins:       plugins,
		},
		Resources: &api.Resources{
			ScalarResources: map[string]float64{
				api.NanoCPUs.String():    float64(info.NCPU) * 1e9,
				api.MemoryBytes.String(): float64(info.MemTotal),
			},
		},
	}

	return description, nil
}

func (e *executor) Configure(ctx context.Context, node *api.Node) error {
	return nil
}

// Controller returns a docker container controller.
func (e *executor) Controller(t *api.Task) (exec.Controller, error) {
	ctlr, err := newController(e.client, t)
	if err != nil {
		return nil, err
	}

	return ctlr, nil
}

func (e *executor) SetNetworkBootstrapKeys([]*api.EncryptionKey) error {
	return nil
}
