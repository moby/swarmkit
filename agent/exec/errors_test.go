package exec

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
)

func TestIsTemporary(t *testing.T) {
	err := fmt.Errorf("err")
	err1 := MakeTemporary(fmt.Errorf("err1: %w", err))
	err2 := fmt.Errorf("err2: %w", err1)
	err3 := errors.Wrap(err2, "err3")
	err4 := fmt.Errorf("err4: %w", err3)
	err5 := errors.Wrap(err4, "err5")

	assert.False(t, IsTemporary(nil), "expected error to not be a temporary error")
	assert.False(t, IsTemporary(err), "expected error to not be a temporary error")
	assert.True(t, IsTemporary(err5), "expected error to be a temporary error")
}
