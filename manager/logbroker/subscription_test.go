package logbroker

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/require"
)

// TestSubscriptionMessageMarshalSnapshot verifies that a subscription message
// remains unchanged between calculating its encoded size and marshaling it.
// This matches the operations performed by gRPC when sending the message.
//
// Regression test for https://github.com/moby/moby/issues/47322.
func TestSubscriptionMessageMarshalSnapshot(t *testing.T) {
	sub := newSubscription(nil, &api.SubscriptionMessage{
		ID: "subscription-id",
	}, nil)

	msg := sub.Message()
	size := msg.Size()

	sub.Close()

	require.NotPanics(t, func() {
		_, err := msg.MarshalToSizedBuffer(make([]byte, size))
		require.NoError(t, err)
	})
}
