package deepcopy

import (
	"google.golang.org/protobuf/proto"
)

// CopierFrom can be implemented if an object knows how to copy another into itself.
type CopierFrom interface {
	// CopyFrom takes the fields from src and copies them into the target object.
	//
	// Calling this method with a nil receiver or a nil src may panic.
	CopyFrom(src any)
}

// Copy copies src into dst, replacing whatever dst held. dst and src must have
// the same type.
//
// Prefer the generated, type-safe Copy method on the message itself; this
// helper only exists for the cases where the concrete type is not known
// statically.
func Copy(dst, src proto.Message) {
	if c, ok := dst.(CopierFrom); ok {
		c.CopyFrom(src)
		return
	}
	proto.Reset(dst)
	proto.Merge(dst, src)
}
