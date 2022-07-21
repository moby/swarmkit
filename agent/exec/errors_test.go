package exec

import (
	"fmt"
	"testing"

	"github.com/pkg/errors"
)

func TestIsTemporary(t *testing.T) {
	err := fmt.Errorf("err")
	err1 := MakeTemporary(fmt.Errorf("err1: %w", err))
	err2 := fmt.Errorf("err2: %w", err1)
	err3 := errors.Wrap(err2, "err3")
	err4 := fmt.Errorf("err4: %w", err3)
	err5 := errors.Wrap(err4, "err5")

	if IsTemporary(nil) {
		t.Error("expected error to not be a temporary error")
	}
	if IsTemporary(err) {
		t.Error("expected error to not be a temporary error")
	}
	if !IsTemporary(err5) {
		t.Error("expected error to be a temporary error")
	}
}
