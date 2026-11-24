package raft

import (
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/stretchr/testify/require"
	"go.etcd.io/raft/v3/raftpb"
)

func TestCreateConfigChangeEntsPreservesSelfMetadata(t *testing.T) {
	self := api.RaftMember{
		RaftID: 42,
		NodeID: "node-id",
		Addr:   "127.0.0.1:2377",
	}

	entries := createConfigChangeEnts(nil, self, 3, 7)
	require.Len(t, entries, 1)
	require.Equal(t, raftpb.EntryConfChange, entries[0].Type)

	var change raftpb.ConfChange
	require.NoError(t, change.Unmarshal(entries[0].Data))
	require.Equal(t, raftpb.ConfChangeAddNode, change.Type)
	require.Equal(t, self.RaftID, change.NodeID)

	var member api.RaftMember
	require.NoError(t, member.Unmarshal(change.Context))
	require.Equal(t, self, member)
}
