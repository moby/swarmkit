package transport

import (
	"math"
	"testing"

	"golang.org/x/net/context"

	"github.com/coreos/etcd/raft/raftpb"
	"github.com/stretchr/testify/assert"
)

// Test SplitSnapshot() for different snapshot sizes.
func TestSplitSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var raftMsg raftpb.Message
	raftMsg.Type = raftpb.MsgSnap
	snaphotSize := 8 << 20
	raftMsg.Snapshot.Data = make([]byte, snaphotSize)

	raftMessagePayloadSize := raftMessagePayloadSize(&raftMsg)

	numMsgs := int(math.Ceil(float64(snaphotSize) / float64(raftMessagePayloadSize)))
	msgs := splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, numMsgs, len(msgs), "unexpected number of messages")

	raftMsg.Snapshot.Data = make([]byte, raftMessagePayloadSize)
	msgs = splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, 1, len(msgs), "unexpected number of messages")

	raftMsg.Snapshot.Data = make([]byte, raftMessagePayloadSize-1)
	msgs = splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, 1, len(msgs), "unexpected number of messages")

	raftMsg.Snapshot.Data = make([]byte, raftMessagePayloadSize*2)
	msgs = splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, 2, len(msgs), "unexpected number of messages")

	raftMsg.Snapshot.Data = make([]byte, 0)
	msgs = splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, len(msgs), 0, "unexpected number of messages")

	raftMsg.Type = raftpb.MsgApp
	msgs = splitSnapshotData(ctx, &raftMsg)
	assert.Equal(t, len(msgs), 0, "unexpected number of messages")
}
