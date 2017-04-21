package template

import (
	"github.com/docker/swarmkit/agent/exec"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
)

type templatedSecretGetter struct {
	dependencies exec.DependencyGetter
	t            *api.Task
}

// NewTemplatedSecretGetter returns a SecretGetter that evaluates templates.
func NewTemplatedSecretGetter(dependencies exec.DependencyGetter, t *api.Task) exec.SecretGetter {
	return templatedSecretGetter{dependencies: dependencies, t: t}
}

func (t templatedSecretGetter) Get(secretID string) *api.Secret {
	if t.dependencies == nil {
		return nil
	}

	secrets := t.dependencies.Secrets()
	if secrets == nil {
		return nil
	}

	secret := secrets.Get(secretID)
	if secret == nil {
		return nil
	}

	newSpec, err := ExpandSecretSpec(secret, t.t, t.dependencies)
	if err != nil {
		log.L.WithError(err).Error("failed to expand templated secret")
		return secret
	}

	secretCopy := *secret
	secretCopy.Spec = *newSpec
	return &secretCopy
}

type templatedConfigGetter struct {
	dependencies exec.DependencyGetter
	t            *api.Task
}

// NewTemplatedConfigGetter returns a ConfigGetter that evaluates templates.
func NewTemplatedConfigGetter(dependencies exec.DependencyGetter, t *api.Task) exec.ConfigGetter {
	return templatedConfigGetter{dependencies: dependencies, t: t}
}

func (t templatedConfigGetter) Get(configID string) *api.Config {
	if t.dependencies == nil {
		return nil
	}

	configs := t.dependencies.Configs()
	if configs == nil {
		return nil
	}

	config := configs.Get(configID)
	if config == nil {
		return nil
	}

	newSpec, err := ExpandConfigSpec(config, t.t, t.dependencies)
	if err != nil {
		log.L.WithError(err).Error("failed to expand templated config")
		return config
	}

	configCopy := *config
	configCopy.Spec = *newSpec
	return &configCopy
}

type templatedDependencyGetter struct {
	secrets exec.SecretGetter
	configs exec.ConfigGetter
}

// NewTemplatedDependencyGetter returns a DependencyGetter that evaluates templates.
func NewTemplatedDependencyGetter(dependencies exec.DependencyGetter, t *api.Task) exec.DependencyGetter {
	return templatedDependencyGetter{
		secrets: NewTemplatedSecretGetter(dependencies, t),
		configs: NewTemplatedConfigGetter(dependencies, t),
	}
}

func (t templatedDependencyGetter) Secrets() exec.SecretGetter {
	return t.secrets
}

func (t templatedDependencyGetter) Configs() exec.ConfigGetter {
	return t.configs
}
