package common

import (
	"fmt"
	"io"
)

// FprintfIfNotEmpty prints only if `s` is not empty.
func FprintfIfNotEmpty(w io.Writer, format string, s string) {
	if s != "" {
		fmt.Fprintf(w, format, s)
	}
}
