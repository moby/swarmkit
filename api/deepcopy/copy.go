package deepcopy

import (
	"fmt"
	"time"

	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// CopierFrom can be implemented if an object knows how to copy another into itself.
type CopierFrom interface {
	// Copy takes the fields from src and copies them into the target object.
	//
	// Calling this method with a nil receiver or a nil src may panic.
	CopyFrom(src any)
}

// Copy copies src into dst. dst and src must have the same type.
//
// If the type has a copy function defined, it will be used.
//
// Default implementations for builtin types and well known protobuf types may
// be provided.
//
// If the copy cannot be performed, this function will panic. Make sure to test
// types that use this function.
func Copy(dst, src any) {
	switch dst := dst.(type) {
	case *anypb.Any:
		src := src.(*anypb.Any)
		dst.TypeUrl = src.TypeUrl
		if src.Value != nil {
			dst.Value = make([]byte, len(src.Value))
			copy(dst.Value, src.Value)
		} else {
			dst.Value = nil
		}
	case *durationpb.Duration:
		src := src.(*durationpb.Duration)
		*dst = *src
	case *time.Duration:
		src := src.(*time.Duration)
		*dst = *src
	case *timestamppb.Timestamp:
		src := src.(*timestamppb.Timestamp)
		*dst = *src
	case *wrapperspb.BoolValue:
		src := src.(*wrapperspb.BoolValue)
		*dst = *src
	case *wrapperspb.Int64Value:
		src := src.(*wrapperspb.Int64Value)
		*dst = *src
	case CopierFrom:
		dst.CopyFrom(src)
	default:
		panic(fmt.Sprintf("Copy for %T not implemented", dst))
	}
}
