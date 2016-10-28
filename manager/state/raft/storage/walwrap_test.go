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
func TestReadAllWrappedNoEncryption(t *testing.T) {
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

	c := NewWALFactory(encryption.NoopCrypter, encryption.NoopCrypter)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer wrapped.Close()

	metaW, _, entsW, err := wrapped.ReadAll()
	require.NoError(t, err)
	require.NoError(t, wrapped.Close())

	require.Equal(t, metadata, metaW)
	require.Equal(t, entries, entsW)
}

// When reading WAL, if the decrypter can't read the encryption type, errors
func TestReadAllNoSupportedDecrypter(t *testing.T) {
	metadata, entries, snapshot := makeWALData()
	for i, entry := range entries {
		r := api.MaybeEncryptedRecord{Data: entry.Data, Algorithm: api.MaybeEncryptedRecord_Algorithm(-3)}
		data, err := r.Marshal()
		require.NoError(t, err)
		entries[i].Data = data
	}

	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	c := NewWALFactory(encryption.NoopCrypter, encryption.NoopCrypter)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)
	defer wrapped.Close()

	_, _, _, err = wrapped.ReadAll()
	require.Error(t, err)
	defer wrapped.Close()
}

// When reading WAL, if a decrypter is available for the encryption type but any
// entry is incorrectly encryptd, an error is returned
func TestReadAllEntryIncorrectlyEncrypted(t *testing.T) {
	crypter := &meowCrypter{}
	metadata, entries, snapshot := makeWALData()

	// metadata is correctly encryptd, but entries are not meow-encryptd
	for i, entry := range entries {
		r := api.MaybeEncryptedRecord{Data: entry.Data, Algorithm: crypter.Algorithm()}
		data, err := r.Marshal()
		require.NoError(t, err)
		entries[i].Data = data
	}

	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	c := NewWALFactory(encryption.NoopCrypter, crypter)
	wrapped, err := c.Open(tempdir, snapshot)
	require.NoError(t, err)

	_, _, _, err = wrapped.ReadAll()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not meowcoded")
	require.NoError(t, wrapped.Close())
}

// The entry data and metadata are encryptd with the given encrypter, and a regular
// WAL will see them as such.
func TestSave(t *testing.T) {
	metadata, entries, snapshot := makeWALData()

	crypter := &meowCrypter{}
	c := NewWALFactory(crypter, encryption.NoopCrypter)
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

		require.Equal(t, crypter.Algorithm(), encrypted.Algorithm)
		require.True(t, bytes.HasSuffix(encrypted.Data, []byte("🐱")))
	}
}

// If encryption fails, saving will fail
func TestSaveEncryptionFails(t *testing.T) {
	metadata, entries, snapshot := makeWALData()

	tempdir, err := ioutil.TempDir("", "waltests")
	require.NoError(t, err)
	defer os.RemoveAll(tempdir)

	// fail encrypting one of the entries, but not the first one
	c := NewWALFactory(&meowCrypter{encryptFailures: map[string]struct{}{
		"Entry 7": {},
	}}, nil)
	wrapped, err := c.Create(tempdir, metadata)
	require.NoError(t, err)

	require.NoError(t, wrapped.SaveSnapshot(snapshot))
	err = wrapped.Save(raftpb.HardState{}, entries)
	require.Error(t, err)
	require.Contains(t, err.Error(), "refusing to encrypt")
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
	c := NewWALFactory(encryption.NoopCrypter, encryption.NoopCrypter)

	_, err := c.Create("/not/existing/directory", []byte("metadata"))
	require.Error(t, err)

	_, err = c.Open("/not/existing/directory", walpb.Snapshot{})
	require.Error(t, err)
}

// A WAL can read what it wrote so long as it has a corresponding decrypter
func TestSaveAndRead(t *testing.T) {
	crypter := &meowCrypter{}
	metadata, entries, snapshot := makeWALData()

	c := NewWALFactory(crypter, crypter)
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
