package ioutils

import (
	"os"

	"github.com/moby/sys/atomicwriter"
)

// AtomicWriteFile atomically writes data to a file specified by filename.
//
// Deprecated: use [atomicwriter.WriteFile].
//
//go:fix inline
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	return atomicwriter.WriteFile(filename, data, perm)
}
