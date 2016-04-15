package common

import (
	"fmt"
	"io"
	"strings"
)

// PrintHeader prints a nice little header.
func PrintHeader(w io.Writer, columns ...string) {
	underline := make([]string, len(columns))
	for i := range underline {
		underline[i] = strings.Repeat("-", len(columns[i]))
	}
	fmt.Fprintf(w, "%s\n", strings.Join(columns, "\t"))
	fmt.Fprintf(w, "%s\n", strings.Join(underline, "\t"))
}

// FprintfIfNotEmpty prints only if `s` is not empty.
//
// NOTE(stevvooe): Not even remotely a printf function.. doesn't take args.
func FprintfIfNotEmpty(w io.Writer, format string, v interface{}) {
	if v != nil && v != "" {
		fmt.Fprintf(w, format, v)
	}
}
