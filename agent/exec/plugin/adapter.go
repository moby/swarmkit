package plugin

import (
	"strings"

	"github.com/docker/docker/api/types"
	engineapi "github.com/docker/docker/client"
	"github.com/docker/swarmkit/api"
	"golang.org/x/net/context"
)

type pluginAdapter struct {
	client engineapi.APIClient
	plugin *pluginConfig
}

func newPluginAdapter(client engineapi.APIClient, task *api.Task) (*pluginAdapter, error) {
	plgn, err := newPluginConfig(task)
	if err != nil {
		return nil, err
	}

	return &pluginAdapter{
		client: client,
		plugin: plgn,
	}, nil
}

func (p *pluginAdapter) install(ctx context.Context) error {
	return p.client.PluginInstall(ctx, p.plugin.image(), types.PluginInstallOptions{
		Disabled:             true,
		AcceptAllPermissions: true,
	})
}

func (p *pluginAdapter) enable(ctx context.Context) error {
	return p.client.PluginEnable(ctx, p.plugin.image())
}

func (p *pluginAdapter) disable(ctx context.Context) error {
	return p.client.PluginDisable(ctx, p.plugin.image())
}

func (p *pluginAdapter) remove(ctx context.Context) error {
	return p.client.PluginRemove(ctx, p.plugin.image(), types.PluginRemoveOptions{})
}

func (p *pluginAdapter) inspect(ctx context.Context) (*types.Plugin, error) {
	plugin, _, err := p.client.PluginInspectWithRaw(ctx, p.plugin.image())
	return plugin, err
}

func isPluginExists(err error) bool {
	return strings.HasSuffix(err.Error(), "exists")
}

func isPluginAlreadyDisabled(err error) bool {
	return strings.HasSuffix(err.Error(), "is already disabled")
}
