package identity

import (
	cryptorand "crypto/rand"
	"fmt"
	"io"
	"math/big"
)

var (
	// idReader is used for random id generation. This declaration allows us to
	// replace it for testing.
	idReader = cryptorand.Reader
)

// parameters for random identifier generation. We can tweak this when there is
// time for further analysis.
const (
	randomIDEntropyBytes = 17
	randomIDBase         = 36

	// To ensure that all identifiers are fixed length, we make sure they
	// get padded out or truncated to 25 characters.
	//
	// For academics,  f5lxx1zz5pnorynqglhzmsp33  == 2^128 - 1. This value
	// was calculated from floor(log(2^128-1, 36)) + 1.
	//
	// While 128 bits is the largest whole-byte size that fits into 25
	// base-36 characters, we generate an extra byte of entropy to fill
	// in the high bits, which would otherwise be 0. This gives us a more
	// even distribution of the first character.
	//
	// See http://mathworld.wolfram.com/NumberLength.html for more information.
	maxRandomIDLength = 25
)

// NewID generates a new identifier for use where random identifiers with low
// collision probability are required.
//
// With the parameters in this package, the generated identifier will provide
// ~129 bits of entropy encoded with base36. Leading padding is added if the
// string is less 25 bytes. We do not intend to maintain this interface, so
// identifiers should be treated opaquely.
func NewID() string {
	var p [randomIDEntropyBytes]byte

	if _, err := io.ReadFull(idReader, p[:]); err != nil {
		panic(fmt.Errorf("failed to read random bytes: %v", err))
	}

	p[0] |= 0x80 // set high bit to avoid the need for padding
	return (&big.Int{}).SetBytes(p[:]).Text(randomIDBase)[1 : maxRandomIDLength+1]
}

// XorIDs Xors two random IDs and produces a third random ID. This third random
// ID can then be reliably xorred back with one of the original IDs to get out
// the other original ID
//
// Note that XorIDs is not guaranteed to produce an ID of the same length as
// NewID, or with the same properties.
func XorIDs(id1, id2 string) string {
	// First, we convert the ids back to big.Int, so we can do math on them
	id1int, success := (&big.Int{}).SetString(id1, randomIDBase)
	if !success {
		panic(fmt.Errorf("Failed to convert first ID to big.Int{}"))
	}
	id2int, success := (&big.Int{}).SetString(id2, randomIDBase)
	if !success {
		panic(fmt.Errorf("Failed to convert second ID to big.Int{}"))
	}

	// Now, we xor the two numbers together.
	xorint := (&big.Int{}).Xor(id1int, id2int)
	// If there are leading 0s in the result, they might get omitted, but we
	// want to keep them affirmatively. Thus, if the resulting string is less
	// than maxRandomIDLength, then we should leading pad with 0s to make it
	// the same length as other IDs.
	xorstr := xorint.Text(randomIDBase)
	for i := len(xorstr); i < maxRandomIDLength; i++ {
		xorstr = "0" + xorstr
	}
	return xorstr
}
