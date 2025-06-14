package identity

import (
	"math/big"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenerateGUID(t *testing.T) {
	idReader = rand.New(rand.NewSource(0))

	for i := 0; i < 1000; i++ {
		guid := NewID()

		var i big.Int
		_, ok := i.SetString(guid, randomIDBase)
		require.True(t, ok, "id should be base 36", i, guid)

		// To ensure that all identifiers are fixed length, we make sure they
		// get padded out to 25 characters, which is the maximum for the base36
		// representation of 128-bit identifiers.
		//
		// For academics,  f5lxx1zz5pnorynqglhzmsp33  == 2^128 - 1. This value
		// was calculated from floor(log(2^128-1, 36)) + 1.
		//
		// See http://mathworld.wolfram.com/NumberLength.html for more information.
		require.Lenf(t, guid, maxRandomIDLength, "len(%s) != %v", guid, maxRandomIDLength)
	}
}
