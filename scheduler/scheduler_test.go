package scheduler

import (
	"testing"

	"github.com/docker/swarm-v2/state"
	"github.com/stretchr/testify/assert"
)

func TestScheduler(t *testing.T) {
	store := state.NewMemoryStore()
	s := New(store)

	assert.NoError(t, s.Start())
	assert.EqualError(t, s.Start(), ErrRunning.Error())

	assert.NoError(t, s.Stop())
	assert.EqualError(t, s.Stop(), ErrNotRunning.Error())
}
