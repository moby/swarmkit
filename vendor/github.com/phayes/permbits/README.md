[![GoDoc](https://godoc.org/github.com/phayes/permbits?status.svg)](https://godoc.org/github.com/phayes/permbits)
[![CircleCI](https://circleci.com/gh/phayes/permbits.svg?style=svg)](https://circleci.com/gh/phayes/permbits)
[![Maintainability](https://api.codeclimate.com/v1/badges/4066ed1d4e9e3c9fc1de/maintainability)](https://codeclimate.com/github/phayes/permbits/maintainability)
[![Test Coverage](https://api.codeclimate.com/v1/badges/4066ed1d4e9e3c9fc1de/test_coverage)](https://codeclimate.com/github/phayes/permbits/test_coverage)
[![patreon](https://img.shields.io/badge/patreon-donate-green.svg)](https://patreon.com/phayes)
[![flattr](https://img.shields.io/badge/flattr-donate-green.svg)](https://flattr.com/@phayes)

# PermBits

Easy file permissions for golang. Easily get and set file permission bits. 

This package makes it a breeze to check and modify file permission bits in Linux, Mac, and other Unix systems. 

## Example

```go
permissions, err := permbits.Stat("/path/to/my/file")
if err != nil {
	return err
}

// Check to make sure the group can write to the file
// If they can't write, update the permissions so they can
if !permissions.GroupWrite() {
	permissions.SetGroupWrite(true)
	err := permbits.Chmod("/path/to/my/file", permissions)
	if err != nil {
		return errors.New("error setting permission on file", err)
	}
}

// Also works well with os.File
fileInfo, err := file.Stat()
if err != nil {
	return err
}
fileMode := fileInfo.Mode()
permissions := permbits.FileMode(fileMode)

// Disable write access to the file for everyone but the user
permissions.SetGroupWrite(false)
permissions.SetOtherWrite(false)
permbits.UpdateFileMode(&fileMode, permissions)

// You can also work with octets directly
if permissions != 0777 {
	return fmt.Errorf("Permissions on file are incorrect. Should be 777, got %o", permissions)
}

```

 ## Contributors
 
 1. Patrick Hayes ([linkedin](https://www.linkedin.com/in/patrickdhayes/)) ([github](https://github.com/phayes)) - Available for hire.
