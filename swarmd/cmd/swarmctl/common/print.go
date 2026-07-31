package common

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/dustin/go-humanize"
	"google.golang.org/protobuf/types/known/timestamppb"
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
func FprintfIfNotEmpty(w io.Writer, format string, v any) {
	if v != nil && v != "" {
		fmt.Fprintf(w, format, v)
	}
}

// TimestampAgo returns a relative time string from a timestamp (e.g. "12 seconds ago").
func TimestampAgo(ts *timestamppb.Timestamp) string {
	if ts == nil {
		return ""
	}
	// AsTime silently returns a bogus time for an out-of-range timestamp,
	// so keep validating it explicitly like the gogo conversion did.
	if err := ts.CheckValid(); err != nil {
		panic(err)
	}
	return humanize.Time(ts.AsTime())
}

// TimestampString formats a timestamp as RFC 3339, or renders the reason it
// could not be formatted.
//
// It replaces gogo's types.TimestampString, which the official well-known
// types do not provide.
func TimestampString(ts *timestamppb.Timestamp) string {
	if err := ts.CheckValid(); err != nil {
		return fmt.Sprintf("(%v)", err)
	}
	return ts.AsTime().Format(time.RFC3339Nano)
}
