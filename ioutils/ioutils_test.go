package ioutils

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteToFile(t *testing.T) {
	tmpDir := t.TempDir()
	expected := []byte("barbaz")
	if err := AtomicWriteFile(filepath.Join(tmpDir, "foo"), expected, 0o600); err != nil {
		t.Fatalf("Error writing to file: %v", err)
	}

	actual, err := os.ReadFile(filepath.Join(tmpDir, "foo"))
	if err != nil {
		t.Fatalf("Error reading from file: %v", err)
	}

	if !bytes.Equal(actual, expected) {
		t.Fatalf("Data mismatch, expected %q, got %q", expected, actual)
	}
}
