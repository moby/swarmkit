package ioutils

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAtomicWriteToFile(t *testing.T) {
	tmpDir := t.TempDir()
	expected := []byte("barbaz")
	err := AtomicWriteFile(filepath.Join(tmpDir, "foo"), expected, 0o600)
	require.NoErrorf(t, err, "Error writing to file: %v", err)

	actual, err := os.ReadFile(filepath.Join(tmpDir, "foo"))
	require.NoErrorf(t, err, "Error reading from file: %v", err)

	require.Truef(t, bytes.Equal(actual, expected), "Data mismatch, expected %q, got %q", expected, actual)
}
