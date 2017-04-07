package ioutils

import (
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
)

// todo: split docker/pkg/ioutils into a separate repo

// AtomicWriteFile atomically writes data to a file specified by filename.
func AtomicWriteFile(filename string, data []byte, perm os.FileMode) error {
	f, err := ioutil.TempFile(filepath.Dir(filename), ".tmp-"+filepath.Base(filename))
	if err != nil {
		return err
	}

	defer f.Close()

	if err := os.Chmod(f.Name(), perm); err != nil {
		return err
	}

	n, err := f.Write(data)
	if err == nil && n < len(data) {
		return io.ErrShortWrite
	}
	if err != nil {
		return err
	}

	if err := f.Sync(); err != nil {
		return err
	}
	return os.Rename(f.Name(), filename)
}
