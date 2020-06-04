// +build tools

// tools.go is a file which contains underscore imports of the tools necessary
// for building swarmkit. This forces package management software, like go mod,
// to recognize these packages as required, even though they are not imported
// into the actual codebase.
package swarmkit

import _ "github.com/stevvooe/protobuild"
