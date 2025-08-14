package drivers

import (
	"errors"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/node/plugin"
)

// DriverProvider provides external drivers
type DriverProvider struct {
	pluginGetter plugin.Getter
}

// New returns a new driver provider
func New(pluginGetter plugin.Getter) *DriverProvider {
	return &DriverProvider{pluginGetter: pluginGetter}
}

// NewSecretDriver creates a new driver for fetching secrets
func (m *DriverProvider) NewSecretDriver(driver *api.Driver) (*SecretDriver, error) {
	if m.pluginGetter == nil {
		return nil, errors.New("plugin getter is nil")
	}
	if driver == nil || driver.Name == "" {
		return nil, errors.New("driver specification is nil")
	}
	// Search for the specified plugin
	plugin, err := m.pluginGetter.Get(driver.Name, SecretsProviderCapability)
	if err != nil {
		return nil, err
	}
	return NewSecretDriver(plugin), nil
}
