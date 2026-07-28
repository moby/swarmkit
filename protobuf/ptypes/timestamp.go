package ptypes

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// MustTimestampProto converts time.Time to a google.protobuf.Timestamp proto.
func MustTimestampProto(t time.Time) *timestamppb.Timestamp {
	return timestamppb.New(t)
}
