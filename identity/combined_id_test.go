package identity

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestCombineTwoIDs(t *testing.T) {
	idReader = rand.New(rand.NewSource(0))
	id1 := NewID()
	id2 := NewID()
	combinedID := CombineTwoIDs(id1, id2)

	expected := fmt.Sprintf("%s.%s", id1, id2)
	if combinedID != expected {
		t.Fatalf("%s != %s", combinedID, expected)
	}
}
