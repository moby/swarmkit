package testutils

import (
	"fmt"
	"net"
	"time"

	"github.com/docker/docker/pkg/plugingetter"
	"github.com/docker/docker/pkg/plugins"
)

// DockerCSIPluginCap is the capability for CSI plugins.
const DockerCSIPluginCap = "csiplugin"

type FakePluginGetter struct {
	Plugins map[string]*FakeCompatPlugin
}

func (f *FakePluginGetter) Get(name, capability string, _ int) (plugingetter.CompatPlugin, error) {
	if capability != DockerCSIPluginCap {
		return nil, fmt.Errorf(
			"requested plugin with %s cap, but should only ever request %s",
			capability, DockerCSIPluginCap,
		)
	}

	if plug, ok := f.Plugins[name]; ok {
		return plug, nil
	}
	return nil, fmt.Errorf("plugin %s not found", name)
}

// GetAllByCap is not needed in the fake and is unimplemented
func (f *FakePluginGetter) GetAllByCap(_ string) ([]plugingetter.CompatPlugin, error) {
	return nil, nil
}

// GetAllManagedPluginsByCap returns all of the fake's plugins. If capability
// is anything other than DockerCSIPluginCap, it returns nothing.
func (f *FakePluginGetter) GetAllManagedPluginsByCap(capability string) []plugingetter.CompatPlugin {
	if capability != DockerCSIPluginCap {
		return nil
	}

	allPlugins := make([]plugingetter.CompatPlugin, 0, len(f.Plugins))
	for _, plug := range f.Plugins {
		allPlugins = append(allPlugins, plug)
	}
	return allPlugins
}

// Handle is not needed in the fake, so is unimplemented.
func (f *FakePluginGetter) Handle(_ string, _ func(string, *plugins.Client)) {}

// fakeCompatPlugin is a fake implementing the plugingetter.CompatPlugin and
// plugingetter.PluginAddr interfaces
type FakeCompatPlugin struct {
	PluginName string
	PluginAddr net.Addr
	Scope      string
}

func (f *FakeCompatPlugin) Name() string {
	return f.PluginName
}

func (f *FakeCompatPlugin) ScopedPath(path string) string {
	if f.Scope != "" {
		return fmt.Sprintf("%s/%s", f.Scope, path)
	}
	return path
}

func (f *FakeCompatPlugin) IsV1() bool {
	return false
}

func (f *FakeCompatPlugin) Client() *plugins.Client {
	return nil
}

func (f *FakeCompatPlugin) Addr() net.Addr {
	return f.PluginAddr
}

func (f *FakeCompatPlugin) Timeout() time.Duration {
	return time.Second
}

func (f *FakeCompatPlugin) Protocol() string {
	return ""
}
