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

func newSecrets() *secrets {
	return &secrets{
		m: make(map[string]*api.Secret),
	}
}

// get returns a secret by ID.  If the secret doesn't exist, returns nil.
func (s *secrets) get(secretID string) *api.Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s, ok := s.m[secretID]; ok {
		return s
	}
	return nil
}

// add adds one or more secrets to the secret map
func (s *secrets) add(secrets ...api.Secret) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, secret := range secrets {
		s.m[secret.ID] = &secret
	}
}

// remove removes one or more secrets by ID from the secret map.  Succeeds
// whether or not the given IDs are in the map.
func (s *secrets) remove(secrets []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, secret := range secrets {
		delete(s.m, secret)
	}
}

// reset removes all the secrets
func (s *secrets) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = make(map[string]*api.Secret)
}

func (s *secrets) filter(secretIDs []string) map[string]*api.Secret {
	s.mu.RLock()
	defer s.mu.RUnlock()
	filteredSecrets := make(map[string]*api.Secret)

	for _, secretID := range secretIDs {
		if s, ok := s.m[secretID]; ok {
			filteredSecrets[secretID] = s.Copy()
		}
	}

	return filteredSecrets
}

// FilterByTask returns only the secrets needed by a specific Task
func (s *secrets) filterByTask(task *api.Task) map[string]*api.Secret {
	var secretIDs []string

	container := task.Spec.GetContainer()
	if container != nil {
		for _, secretRef := range container.Secrets {
			secretIDs = append(secretIDs, secretRef.SecretID)
		}
	}

	return s.filter(secretIDs)
}
