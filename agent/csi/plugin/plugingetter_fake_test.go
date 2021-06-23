package plugin

import (
	"fmt"
	"net"
	"time"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/docker/pkg/plugins"
)

type fakePluginGetter struct {
	plugins map[string]*fakeCompatPlugin
}

func (f *fakePluginGetter) Get(name, capability string, _ int) (plugingetter.CompatPlugin, error) {
	if capability != DockerCSIPluginCap {
		return nil, fmt.Errorf(
			"requested plugin with %s cap, but should only ever request %s",
			capability, DockerCSIPluginCap,
		)
	}
	if plug, ok := f.plugins[name]; ok {
		return plug, nil
	}
	return nil, fmt.Errorf("plugin %s not found", name)
}

// GetAllByCap is not needed in the fake and is unimplemented
func (f *fakePluginGetter) GetAllByCap(_ string) ([]plugingetter.CompatPlugin, error) {
	return nil, nil
}

// GetAllManagedPluginsByCap returns all of the fake's plugins. If capability
// is anything other than DockerCSIPluginCap, it returns nothing.
func (f *fakePluginGetter) GetAllManagedPluginsByCap(capability string) []plugingetter.CompatPlugin {
	if capability != DockerCSIPluginCap {
		return nil
	}

	allPlugins := make([]plugingetter.CompatPlugin, 0, len(f.plugins))
	for _, plug := range f.plugins {
		allPlugins = append(allPlugins, plug)
	}
	return allPlugins
}

// Handle is not needed in the fake, so is unimplemented.
func (f *fakePluginGetter) Handle(_ string, _ func(string, *plugins.Client)) {}

// fakeCompatPlugin is a fake implementing the plugingetter.CompatPlugin and
// plugingetter.PluginAddr interfaces
type fakeCompatPlugin struct {
	name string
	addr net.Addr
}

func (f *fakeCompatPlugin) Name() string {
	return f.name
}

func (f *fakeCompatPlugin) ScopedPath(_ string) string {
	return ""
}

func (f *fakeCompatPlugin) IsV1() bool {
	return false
}

func (f *fakeCompatPlugin) Client() *plugins.Client {
	return nil
}

func (f *fakeCompatPlugin) Addr() net.Addr {
	return f.addr
}

func (f *fakeCompatPlugin) Timeout() time.Duration {
	return time.Second
}

func (f *fakeCompatPlugin) Protocol() string {
	return ""
}
