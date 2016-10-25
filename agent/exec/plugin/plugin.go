package plugin

import (
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
)

// pluginConfig converts task properties into docker plugin compatible
// components.
type pluginConfig struct {
	task *api.Task
}

// newPluginConfig returns a validated plugin config. No methods should
// return an error if this function returns without error.
func newPluginConfig(t *api.Task) (*pluginConfig, error) {
	var p pluginConfig
	return &p, p.setTask(t)
}

func (p *pluginConfig) setTask(t *api.Task) error {
	plugin := t.Spec.GetPlugin()
	if plugin == nil {
		return exec.ErrRuntimeUnsupported
	}

	if plugin.Image == "" {
		return exec.ErrImageRequired
	}

	p.task = t
	return nil
}

func (p *pluginConfig) spec() *api.PluginSpec {
	return p.task.Spec.GetPlugin()
}

func (p *pluginConfig) image() string {
	return p.spec().Image
}

func (p *pluginConfig) disabled() bool {
	return p.spec().Disabled
}
