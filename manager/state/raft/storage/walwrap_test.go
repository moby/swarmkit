package storage

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/wal/walpb"
	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/encryption"
	"github.com/stretchr/testify/require"
)

var _ WALFactory = walCryptor{}

// Generates a bunch of WAL test data
func makeWALData() ([]byte, []raftpb.Entry, walpb.Snapshot) {
	term := uint64(3)
	index := uint64(4)

	var entries []raftpb.Entry
	for i := index + 1; i < index+6; i++ {
		entries = append(entries, raftpb.Entry{
			Term:  term,
			Index: i,
			Data:  []byte(fmt.Sprintf("Entry %d", i)),
		})
	}

	return []byte("metadata"), entries, walpb.Snapshot{Index: index, Term: term}
}

func createWithWAL(t *testing.T, w WALFactory, metadata []byte, startSnap walpb.Snapshot, entries []raftpb.Entry) string {
	walDir, err := ioutil.TempDir("", "waltests")
	require.NoError(t, err)

	walWriter, err := w.Create(walDir, metadata)
	require.NoError(t, err)

	require.NoError(t, walWriter.SaveSnapshot(startSnap))
	require.NoError(t, walWriter.Save(raftpb.HardState{}, entries))
	require.NoError(t, walWriter.Close())

	return walDir
}

// WAL can read entries are not wrapped, but not encrypted
func TestReadAllWrappedNoEncoding(t *testing.T) {
	metadata, entries, snapshot := makeWALData()
	wrappedEntries := make([]raftpb.Entry, len(entries))
	for i, entry := range entries {
		r := api.MaybeEncryptedRecord{Data: entry.Data}
		data, err := r.Marshal()
		require.NoError(t, err)
		entry.Data = data
		wrappedEntries[i] = entry
	}

	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, wrappedEntries)
	defer os.RemoveAll(tempdir)

	c := NewWALFactory(encryption.NoopCoder, encryption.NoopCoder)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer wrapped.Close()

	metaW, _, entsW, err := wrapped.ReadAll()
	require.NoError(t, err)
	require.NoError(t, wrapped.Close())

	require.Equal(t, metadata, metaW)
	require.Equal(t, entries, entsW)
}

// When reading WAL, if the decoder can't read the encoding type, errors
func TestReadAllNoSupportedDecoder(t *testing.T) {
	metadata, entries, snapshot := makeWALData()
	for i, entry := range entries {
		r := api.MaybeEncryptedRecord{Data: entry.Data, Algorithm: api.MaybeEncryptedRecord_Algorithm(-3)}
		data, err := r.Marshal()
		require.NoError(t, err)
		entries[i].Data = data
	}

	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	c := NewWALFactory(encryption.NoopCoder, encryption.NoopCoder)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer wrapped.Close()

	_, _, _, err = wrapped.ReadAll()
	require.Error(t, err)
	defer wrapped.Close()
}

// When reading WAL, if a decoder is available for the encoding type but any
// entry is incorrectly encoded, an error is returned
func TestReadAllEntryIncorrectlyEncoded(t *testing.T) {
	coder := &meowCoder{}
	metadata, entries, snapshot := makeWALData()

	// metadata is correctly encoded, but entries are not meow-encoded
	for i, entry := range entries {
		r := api.MaybeEncryptedRecord{Data: entry.Data, Algorithm: coder.Algorithm()}
		data, err := r.Marshal()
		require.NoError(t, err)
		entries[i].Data = data
	}

	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	c := NewWALFactory(encryption.NoopCoder, coder)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)

	_, _, _, err = wrapped.ReadAll()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not meowcoded")
	require.NoError(t, wrapped.Close())
}

// The entry data and metadata are encoded with the given encoder, and a regular
// WAL will see them as such.
func TestSave(t *testing.T) {
	metadata, entries, snapshot := makeWALData()

	coder := &meowCoder{}
	c := NewWALFactory(coder, encryption.NoopCoder)
	tempdir := createWithWAL(t, c, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	ogWAL, err := OriginalWAL.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer ogWAL.Close()

	meta, state, ents, err := ogWAL.ReadAll()
	require.NoError(t, err)
	require.Equal(t, metadata, meta)
	require.Equal(t, state, state)
	for _, ent := range ents {
		var encrypted api.MaybeEncryptedRecord
		require.NoError(t, encrypted.Unmarshal(ent.Data))

		require.Equal(t, coder.Algorithm(), encrypted.Algorithm)
		require.True(t, bytes.HasSuffix(encrypted.Data, []byte("🐱")))
	}
}

// If encoding fails, saving will fail
func TestSaveEncodingFails(t *testing.T) {
	metadata, entries, snapshot := makeWALData()

	tempdir, err := ioutil.TempDir("", "waltests")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// the first encoding is the metadata, so that should succeed - fail on one
	// of the entries, and not the first one
	c := NewWALFactory(&meowCoder{encodeFailures: map[string]struct{}{
		"Entry 7": {},
	}}, nil)
	wrapped, err := c.Create(tempdir, metadata)
	require.NoError(t, err)

	require.NoError(t, wrapped.SaveSnapshot(snapshot))
	err = wrapped.Save(raftpb.HardState{}, entries)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to encode")
	require.NoError(t, wrapped.Close())

	// no entries are written at all
	ogWAL, err := OriginalWAL.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer ogWAL.Close()

	_, _, ents, err := ogWAL.ReadAll()
	require.NoError(t, err)
	require.Empty(t, ents)
}

// If the underlying WAL returns an error when opening or creating, the error
// is propagated up.
func TestCreateOpenInvalidDirFails(t *testing.T) {
	c := NewWALFactory(encryption.NoopCoder, encryption.NoopCoder)

	_, err := c.Create("/not/existing/directory", []byte("metadata"))
	require.Error(t, err)

	_, err = c.Open("/not/existing/directory", walpb.Snapshot{})
	require.Error(t, err)
}

// A WAL can read what it wrote so long as it has a corresponding decoder
func TestSaveAndRead(t *testing.T) {
	coder := &meowCoder{}
	metadata, entries, snapshot := makeWALData()

	c := NewWALFactory(coder, coder)
	tempdir := createWithWAL(t, c, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)

	meta, _, ents, err := wrapped.ReadAll()
	require.NoError(t, wrapped.Close())
	require.NoError(t, err)
	require.Equal(t, metadata, meta)
	require.Equal(t, entries, ents)
}
