package identity

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCombineTwoIDs(t *testing.T) {
	idReader = rand.New(rand.NewSource(0))
	id1 := NewID()
	id2 := NewID()
	combinedID := CombineTwoIDs(id1, id2)

	expected := fmt.Sprintf("%s.%s", id1, id2)
	require.Equalf(t, expected, combinedID, "%s != %s", combinedID, expected)
}
