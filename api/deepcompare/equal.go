package deepcompare

import (
	"fmt"
	"time"
)

// Comparable can be implemented if an object knows how to compare another into itself.
type Comparable interface {
	Equal(other interface{}) bool
}

// Equal does a deep compare between msg1 and msg2.
//
// If the type has an Equal function defined, it will be used.
//
// Default implementations for builtin types and well known protobuf types may
// be provided.
//
// If the comparison cannot be performed, this function will panic. Make sure
// to test types that use this function.
func Equal(msg1, msg2 interface{}) bool {
	switch msg1 := msg1.(type) {
	case *time.Duration:
		msg2 := msg2.(*time.Duration)
		if msg1 == nil || msg2 == nil {
			return msg1 == msg2
		}
		return *msg1 == *msg2
	case Comparable:
		return msg1.Equal(msg2)
	default:
		panic(fmt.Sprintf("Equal for %T not implemented", msg1))
	}
}
