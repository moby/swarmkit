package container

import (
	"strings"

	engineapi "github.com/docker/engine-api/client"
	"github.com/docker/swarm-v2/agent/exec"
	"github.com/docker/swarm-v2/api"
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

	// convert plugins obtained into PluginDescription format
	pluginsList := []*api.PluginDescription{
		{
			Type:  "Volume",
			Names: info.Plugins.Volume,
		},
		{
			Type:  "Network",
			Names: info.Plugins.Network,
		},
		{
			Type:  "Authorization",
			Names: info.Plugins.Authorization,
		},
	}

	// parse []string labels into a map[string]string
	engineLabels := map[string]string{}
	for _, l := range info.Labels {
		stringSlice := strings.Split(l, "=")
		// this will take the last value in the list for a given key
		// ideally, one shouldn't assign multiple values to the same key
		if len(stringSlice) == 2 {
			engineLabels[stringSlice[0]] = stringSlice[1]
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
			Labels:        engineLabels,
			Plugins:       pluginsList,
		},
		Resources: &api.Resources{
			NanoCPUs:    int64(info.NCPU) * 1e9,
			MemoryBytes: info.MemTotal,
		},
	}

	return description, nil
}

// Controller returns a docker container controller.
func (e *executor) Controller(t *api.Task) (exec.Controller, error) {
	ctlr, err := newController(e.client, t)
	if err != nil {
		return nil, err
	}

	return ctlr, nil
}
