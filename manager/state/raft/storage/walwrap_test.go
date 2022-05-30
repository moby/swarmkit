package storage

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/manager/encryption"
	"github.com/stretchr/testify/require"
	"go.etcd.io/etcd/raft/v3/raftpb"
	"go.etcd.io/etcd/server/v3/wal/walpb"
)

var _ WALFactory = walCryptor{}

var (
	confState = raftpb.ConfState{
		Voters:    []uint64{0x00ffca74},
		AutoLeave: false,
	}
)

// Generates a bunch of WAL test data
func makeWALData(index uint64, term uint64, state *raftpb.ConfState) ([]byte, []raftpb.Entry, walpb.Snapshot) {
	wsn := walpb.Snapshot{
		Index:     index,
		Term:      term,
		ConfState: state,
	}

	var entries []raftpb.Entry
	for i := wsn.Index + 1; i < wsn.Index+6; i++ {
		entries = append(entries, raftpb.Entry{
			Term:  wsn.Term + 1,
			Index: i,
			Data:  []byte(fmt.Sprintf("Entry %d", i)),
		})
	}

	return []byte("metadata"), entries, wsn
}

func createWithWAL(t *testing.T, w WALFactory, metadata []byte, startSnap walpb.Snapshot, entries []raftpb.Entry) string {
	walDir, err := os.MkdirTemp("", "waltests")
	require.NoError(t, err)
	require.NoError(t, os.RemoveAll(walDir))

	walWriter, err := w.Create(walDir, metadata)
	require.NoError(t, err)

	require.NoError(t, walWriter.SaveSnapshot(startSnap))
	require.NoError(t, walWriter.Save(raftpb.HardState{}, entries))
	require.NoError(t, walWriter.Close())

	return walDir
}

// WAL can read entries are not wrapped, but not encrypted
func TestReadAllWrappedNoEncryption(t *testing.T) {
	metadata, entries, snapshot := makeWALData(1, 1, &confState)
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
	metadata, entries, snapshot := makeWALData(1, 1, &confState)
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
	metadata, entries, snapshot := makeWALData(1, 1, &confState)

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
	metadata, entries, snapshot := makeWALData(1, 1, &confState)

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
	metadata, entries, snapshot := makeWALData(1, 1, &confState)

	tempdir, err := os.MkdirTemp("", "waltests")
	require.NoError(t, err)
	os.RemoveAll(tempdir)
	defer os.RemoveAll(tempdir)

	// fail encrypting one of the entries, but not the first one
	c := NewWALFactory(&meowCrypter{encryptFailures: map[string]struct{}{
		"Entry 3": {},
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

	tempDir, err := os.MkdirTemp("", "test-migrate")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	_, err = c.Open(tempDir, walpb.Snapshot{}) // invalid because no WAL file
	require.Error(t, err)
}

// A WAL can read what it wrote so long as it has a corresponding decrypter
func TestSaveAndRead(t *testing.T) {
	crypter := &meowCrypter{}
	metadata, entries, snapshot := makeWALData(1, 1, &confState)

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

func TestReadRepairWAL(t *testing.T) {
	metadata, entries, snapshot := makeWALData(1, 1, &confState)
	tempdir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(tempdir)

	// there should only be one WAL file in there - corrupt it
	files, err := os.ReadDir(tempdir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	fName := filepath.Join(tempdir, files[0].Name())
	fileContents, err := os.ReadFile(fName)
	require.NoError(t, err)
	info, err := files[0].Info()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(fName, fileContents[:200], info.Mode()))

	ogWAL, err := OriginalWAL.Open(tempdir, snapshot)
	require.NoError(t, err)
	_, _, _, err = ogWAL.ReadAll()
	require.Error(t, err)
	require.NoError(t, ogWAL.Close())

	ogWAL, waldata, err := ReadRepairWAL(context.Background(), tempdir, snapshot, OriginalWAL)
	require.NoError(t, err)
	require.Equal(t, metadata, waldata.Metadata)
	require.NoError(t, ogWAL.Close())
}

func TestMigrateWALs(t *testing.T) {
	metadata, entries, snapshot := makeWALData(1, 1, &confState)
	coder := &meowCrypter{}
	c := NewWALFactory(coder, coder)

	var (
		err  error
		dirs = make([]string, 2)
	)

	tempDir, err := os.MkdirTemp("", "test-migrate")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	for i := range dirs {
		dirs[i] = filepath.Join(tempDir, fmt.Sprintf("walDir%d", i))
	}

	origDir := createWithWAL(t, OriginalWAL, metadata, snapshot, entries)
	defer os.RemoveAll(origDir)

	// original to new
	oldDir := origDir
	newDir := dirs[0]

	err = MigrateWALs(context.Background(), oldDir, newDir, OriginalWAL, c, snapshot)
	require.NoError(t, err)

	newWAL, err := c.Open(newDir, snapshot)
	require.NoError(t, err)
	meta, _, ents, err := newWAL.ReadAll()
	require.NoError(t, err)
	require.Equal(t, metadata, meta)
	require.Equal(t, entries, ents)
	require.NoError(t, newWAL.Close())

	// new to original
	oldDir = dirs[0]
	newDir = dirs[1]

	err = MigrateWALs(context.Background(), oldDir, newDir, c, OriginalWAL, snapshot)
	require.NoError(t, err)

	newWAL, err = OriginalWAL.Open(newDir, snapshot)
	require.NoError(t, err)
	meta, _, ents, err = newWAL.ReadAll()
	require.NoError(t, err)
	require.Equal(t, metadata, meta)
	require.Equal(t, entries, ents)
	require.NoError(t, newWAL.Close())

	// If we can't read the old directory (for instance if it doesn't exist), a temp directory
	// is not created
	for _, dir := range dirs {
		require.NoError(t, os.RemoveAll(dir))
	}
	oldDir = dirs[0]
	newDir = dirs[1]

	err = MigrateWALs(context.Background(), oldDir, newDir, OriginalWAL, c, walpb.Snapshot{})
	require.Error(t, err)

	subdirs, err := os.ReadDir(tempDir)
	require.NoError(t, err)
	require.Empty(t, subdirs)
}
