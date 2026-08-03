package storage

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/moby/swarmkit/v2/api"
	"github.com/moby/swarmkit/v2/manager/encryption"
	"github.com/stretchr/testify/require"
	"go.etcd.io/raft/v3/raftpb"
	"google.golang.org/protobuf/proto"
)

// Common test utilities

// requireEqualProto asserts that two protobuf messages have the same contents.
// require.Equal cannot be used, because it falls back to reflect.DeepEqual,
// which also walks the internal bookkeeping fields of a protobuf-go message.
func requireEqualProto(t *testing.T, expected, actual proto.Message) {
	t.Helper()
	require.True(t, proto.Equal(expected, actual), "expected:\n%v\nactual:\n%v", expected, actual)
}

// requireEqualEntries asserts that two slices of raft entries have the same
// contents.  See [requireEqualProto].
func requireEqualEntries(t *testing.T, expected, actual []*raftpb.Entry) {
	t.Helper()
	require.Len(t, actual, len(expected))
	for i := range expected {
		requireEqualProto(t, expected[i], actual[i])
	}
}

type meowCrypter struct {
	// only take encryption failures - decrypt failures can happen if the bytes
	// do not have a cat
	encryptFailures map[string]struct{}
}

func (m meowCrypter) Encrypt(orig []byte) (*api.MaybeEncryptedRecord, error) {
	if _, ok := m.encryptFailures[string(orig)]; ok {
		return nil, fmt.Errorf("refusing to encrypt")
	}
	return &api.MaybeEncryptedRecord{
		Algorithm: m.Algorithm(),
		Data:      append(orig, []byte("🐱")...),
	}, nil
}

func (m meowCrypter) Decrypt(orig *api.MaybeEncryptedRecord) ([]byte, error) {
	if orig.Algorithm != m.Algorithm() || !bytes.HasSuffix(orig.Data, []byte("🐱")) {
		return nil, fmt.Errorf("not meowcoded")
	}
	return bytes.TrimSuffix(orig.Data, []byte("🐱")), nil
}

func (m meowCrypter) Algorithm() api.MaybeEncryptedRecord_Algorithm {
	return api.MaybeEncryptedRecord_Algorithm(-1)
}

var _ encryption.Encrypter = meowCrypter{}
var _ encryption.Decrypter = meowCrypter{}
