package context

import (
	"context"
	"os"
	"strconv"
	"strings"

	"google.golang.org/grpc/metadata"
)

// RequestIDKey is the key used to put/get a CSI request ID
// in/fromt a Go context.
const RequestIDKey = "csi.requestid"

var (
	// ctxRequestIDKey is an interface-wrapped key used to access the
	// gRPC request ID injected into an outgoing or incoming context
	// via the GoCSI request ID injection interceptor
	ctxRequestIDKey = interface{}("x-csi-request-id")

	// ctxOSEnviron is an interface-wrapped key used to access a string
	// slice that contains one or more environment variables stored as
	// KEY=VALUE.
	ctxOSEnviron = interface{}("os.Environ")

	// ctxOSLookupEnvKey is an interface-wrapped key used to access a function
	// with the signature func(string) (string, bool) that returns the value of
	// an environment variable.
	ctxOSLookupEnvKey = interface{}("os.LookupEnv")

	// ctxOSSetenvKey is an interface-wrapped key used to access a function
	// with the signature func(string, string) that can be used to set the
	// value of an environment variable
	ctxOSSetenvKey = interface{}("os.Setenev")
)

type lookupEnvFunc func(string) (string, bool)
type setenvFunc func(string, string) error

// GetRequestID inspects the context for gRPC metadata and returns
// its request ID if available.
func GetRequestID(ctx context.Context) (uint64, bool) {
	var (
		szID   []string
		szIDOK bool
	)

	// Prefer the incoming context, but look in both types.
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		szID, szIDOK = md[RequestIDKey]
	} else if md, ok := metadata.FromOutgoingContext(ctx); ok {
		szID, szIDOK = md[RequestIDKey]
	}

	if szIDOK && len(szID) == 1 {
		if id, err := strconv.ParseUint(szID[0], 10, 64); err == nil {
			return id, true
		}
	}

	return 0, false
}

// WithEnviron returns a new Context with the provided environment variable
// string slice.
func WithEnviron(ctx context.Context, v []string) context.Context {
	return context.WithValue(ctx, ctxOSEnviron, v)
}

// WithLookupEnv returns a new Context with the provided function.
func WithLookupEnv(ctx context.Context, f lookupEnvFunc) context.Context {
	return context.WithValue(ctx, ctxOSLookupEnvKey, f)
}

// WithSetenv returns a new Context with the provided function.
func WithSetenv(ctx context.Context, f setenvFunc) context.Context {
	return context.WithValue(ctx, ctxOSSetenvKey, f)
}

// LookupEnv returns the value of the provided environment variable by:
//
//   1. Inspecting the context for a key "os.Environ" with a string
//      slice value. If such a key and value exist then the string slice
//      is searched for the specified key and if found its value is returned.
//
//   2. Inspecting the context for a key "os.LookupEnv" with a value of
//      func(string) (string, bool). If such a key and value exist then the
//      function is used to attempt to discover the key's value. If the
//      key and value are found they are returned.
//
//   3. Returning the result of os.LookupEnv.
func LookupEnv(ctx context.Context, key string) (string, bool) {
	if s, ok := ctx.Value(ctxOSEnviron).([]string); ok {
		for _, v := range s {
			p := strings.SplitN(v, "=", 2)
			if len(p) > 0 && strings.EqualFold(p[0], key) {
				if len(p) > 1 {
					return p[1], true
				}
				return "", true
			}
		}
	}
	if f, ok := ctx.Value(ctxOSLookupEnvKey).(lookupEnvFunc); ok {
		if v, ok := f(key); ok {
			return v, true
		}
	}
	return os.LookupEnv(key)
}

// Getenv is an alias for LookupEnv and drops the boolean return value.
func Getenv(ctx context.Context, key string) string {
	val, _ := LookupEnv(ctx, key)
	return val
}

// Setenv sets the value of the provided environment variable to the
// specified value by first inspecting the context for a key "os.Setenv"
// with a value of func(string, string) error. If the context does not
// contain such a function then os.Setenv is used instead.
func Setenv(ctx context.Context, key, val string) error {
	if f, ok := ctx.Value(ctxOSSetenvKey).(setenvFunc); ok {
		return f(key, val)
	}
	return os.Setenv(key, val)
}
