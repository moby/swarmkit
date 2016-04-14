package common

import (
	"fmt"
	"io"
)

// FprintfIfNotEmpty prints only if `s` is not empty.
//
// NOTE(stevvooe): Not even remotely a printf function.. doesn't take args.
func FprintfIfNotEmpty(w io.Writer, format string, v interface{}) {
	if v != nil && v != "" {
		fmt.Fprintf(w, format, v)
	}
}
