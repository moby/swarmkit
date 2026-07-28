package equality

import (
	"crypto/subtle"

	"google.golang.org/protobuf/proto"

	"github.com/moby/swarmkit/v2/api"
)

// TasksEqualStable returns true if the tasks are functionally equal, ignoring status,
// version and other superfluous fields.
//
// This used to decide whether or not to propagate a task update to a controller.
func TasksEqualStable(a, b *api.Task) bool {
	copyA, copyB := a.Copy(), b.Copy()

	copyA.Status, copyB.Status = nil, nil
	copyA.Meta, copyB.Meta = nil, nil

	return proto.Equal(copyA, copyB)
}

// TaskStatusesEqualStable compares the task status excluding timestamp fields.
func TaskStatusesEqualStable(a, b *api.TaskStatus) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	copyA, copyB := a.Copy(), b.Copy()

	copyA.Timestamp, copyB.Timestamp = nil, nil
	copyA.AppliedAt, copyB.AppliedAt = nil, nil
	return proto.Equal(copyA, copyB)
}

// RootCAEqualStable compares RootCAs, excluding join tokens, which are randomly generated
func RootCAEqualStable(a, b *api.RootCA) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}

	var aRotationKey, bRotationKey []byte
	if a.RootRotation != nil {
		aRotationKey = a.RootRotation.CAKey
	}
	if b.RootRotation != nil {
		bRotationKey = b.RootRotation.CAKey
	}
	if subtle.ConstantTimeCompare(a.CAKey, b.CAKey) != 1 || subtle.ConstantTimeCompare(aRotationKey, bRotationKey) != 1 {
		return false
	}

	copyA, copyB := a.Copy(), b.Copy()
	copyA.JoinTokens, copyB.JoinTokens = nil, nil
	return proto.Equal(copyA, copyB)
}

// ExternalCAsEqualStable compares lists of external CAs and determines whether they are equal.
func ExternalCAsEqualStable(a, b []*api.ExternalCA) bool {
	// because proto.Equal handles nil lists and empty lists differently, check lengths first
	if len(a) == 0 && len(b) == 0 {
		return true
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !proto.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}
