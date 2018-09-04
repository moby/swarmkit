package identity

import (
	"math/big"
	"math/rand"
	"testing"
)

func TestGenerateGUID(t *testing.T) {
	idReader = rand.New(rand.NewSource(0))

	for i := 0; i < 1000; i++ {
		guid := NewID()

		var i big.Int
		_, ok := i.SetString(guid, randomIDBase)
		if !ok {
			t.Fatal("id should be base 36", i, guid)
		}

		// To ensure that all identifiers are fixed length, we make sure they
		// get padded out to 25 characters, which is the maximum for the base36
		// representation of 128-bit identifiers.
		//
		// For academics,  f5lxx1zz5pnorynqglhzmsp33  == 2^128 - 1. This value
		// was calculated from floor(log(2^128-1, 36)) + 1.
		//
		// See http://mathworld.wolfram.com/NumberLength.html for more information.
		if len(guid) != maxRandomIDLength {
			t.Fatalf("len(%s) != %v", guid, maxRandomIDLength)
		}
	}
}

func TestXorIDs(t *testing.T) {
	idReader = rand.New(rand.NewSource(0))
	for i := 0; i < 1000; i++ {
		id1 := NewID()
		id2 := NewID()

		xor := XorIDs(id1, id2)

		t.Logf("\n-- iteration %v --\n", i)
		t.Logf("id1: %v, id2: %v, xor: %v", id1, id2, xor)
		t.Logf("in binary:")

		var id1int, id2int, xorint big.Int
		id1int.SetString(id1, randomIDBase)
		id2int.SetString(id2, randomIDBase)
		xorint.SetString(xor, randomIDBase)

		t.Logf("\tid1: %130v", id1int.Text(2))
		t.Logf("\tid2: %130v", id2int.Text(2))
		t.Logf("\txor: %130v", xorint.Text(2))

		var i big.Int
		_, ok := i.SetString(xor, randomIDBase)
		if !ok {
			t.Fatal("xor result should be base 36")
		}

		// the length issue is a result of the truncation and shift in the generation of ids
		// it means that some values that fit in the same number of bytes do _not_ fit in the same number of base36 characters
		/*
			if len(xor) < maxRandomIDLength {
				t.Errorf("len(%s) (%d) != %v", xor, len(xor), maxRandomIDLength)
			}
		*/

		unxor1 := XorIDs(xor, id1)
		if unxor1 != id2 {
			t.Errorf(
				"unxoring by id1 != id2 (%s ^ %s != %s, got %s)",
				xor, id1, id2, unxor1,
			)
		}

		unxor2 := XorIDs(xor, id2)
		if unxor2 != id1 {
			t.Errorf(
				"unxoring by id2 != id1 (%s ^ %s != %s, got %v)",
				xor, id2, id1, unxor2,
			)
		}
	}
}
