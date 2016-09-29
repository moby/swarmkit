package agent

import (
	"sync"

	"github.com/docker/swarmkit/api"
)

// secrets is a map that keeps all the currenty available secrets to the agent
// mapped by secret ID
type secrets struct {
	mu sync.RWMutex
	m  map[string]*api.Secret
}

func newSecrets() secrets {
	return secrets{
		m: make(map[string]*api.Secret),
	}
}

// GetSecret returns a secret by ID.  If the secret doesn't exist, returns nil.
func (s *secrets) GetSecret(secretID string) *api.Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m[secretID]
}

// AddSecret adds one or more secrets to the secret map
func (s *secrets) AddSecret(secrets ...*api.Secret) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, secret := range secrets {
		s.m[secret.ID] = secret
	}
}

// RemoveSecret removes one or more secrets by ID from the secret map.  Succeeds
// whether or not the given IDs are in the map.
func (s *secrets) RemoveSecret(secrets []*api.Secret) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, secret := range secrets {
		delete(s.m, secret.ID)
	}
}

// Reset removes all the secrets
func (s *secrets) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]*api.Secret)
}
