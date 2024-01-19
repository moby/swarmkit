package genericresource

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/assert"
)

func TestHasResourceDiscreteMoreThanEnough(t *testing.T) {
	required := NewDiscrete("apple", 1)
	available := []*api.GenericResource{NewDiscrete("apple", 5)}

	assert.True(t, HasResource(required, available))
}

func TestHasResourceDiscreteExactlyEnough(t *testing.T) {
	required := NewDiscrete("apple", 5)
	available := []*api.GenericResource{NewDiscrete("apple", 5)}

	assert.True(t, HasResource(required, available))
}

func TestHasResourceDiscreteNotEnough(t *testing.T) {
	required := NewDiscrete("apple", 6)
	available := []*api.GenericResource{NewDiscrete("apple", 5)}

	assert.False(t, HasResource(required, available))
}
