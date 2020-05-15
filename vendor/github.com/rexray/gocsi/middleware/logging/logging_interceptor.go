package logging

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"reflect"
	"regexp"
	"strings"

	"golang.org/x/net/context"
	"google.golang.org/grpc"

	csictx "github.com/rexray/gocsi/context"
	"github.com/rexray/gocsi/utils"
)

// Option configures the logging interceptor.
type Option func(*opts)

type opts struct {
	reqw             io.Writer
	repw             io.Writer
	disableLogVolCtx bool
}

// WithRequestLogging is a Option that enables request logging
// for the logging interceptor.
func WithRequestLogging(w io.Writer) Option {
	return func(o *opts) {
		if w == nil {
			w = os.Stdout
		}
		o.reqw = w
	}
}

// WithResponseLogging is a Option that enables response logging
// for the logging interceptor.
func WithResponseLogging(w io.Writer) Option {
	return func(o *opts) {
		if w == nil {
			w = os.Stdout
		}
		o.repw = w
	}
}

// WithDisableLogVolumeContext is an Option that disables logging the VolumeContext
// field in the logging interceptor
func WithDisableLogVolumeContext() Option {
	return func(o *opts) {
		o.disableLogVolCtx = true
	}
}

type interceptor struct {
	opts opts
}

// NewServerLogger returns a new UnaryServerInterceptor that can be
// configured to log both request and response data.
func NewServerLogger(
	opts ...Option) grpc.UnaryServerInterceptor {

	return newLoggingInterceptor(opts...).handleServer
}

// NewClientLogger provides a UnaryClientInterceptor that can be
// configured to log both request and response data.
func NewClientLogger(
	opts ...Option) grpc.UnaryClientInterceptor {

	return newLoggingInterceptor(opts...).handleClient
}

func newLoggingInterceptor(opts ...Option) *interceptor {
	i := &interceptor{}
	for _, withOpts := range opts {
		withOpts(&i.opts)
	}
	return i
}

func (s *interceptor) handleServer(
	ctx context.Context,
	req interface{},
	info *grpc.UnaryServerInfo,
	handler grpc.UnaryHandler) (interface{}, error) {

	return s.handle(ctx, info.FullMethod, req, func() (interface{}, error) {
		return handler(ctx, req)
	})
}

func (s *interceptor) handleClient(
	ctx context.Context,
	method string,
	req, rep interface{},
	cc *grpc.ClientConn,
	invoker grpc.UnaryInvoker,
	opts ...grpc.CallOption) error {

	_, err := s.handle(ctx, method, req, func() (interface{}, error) {
		return rep, invoker(ctx, method, req, rep, cc, opts...)
	})
	return err
}

func (s *interceptor) handle(
	ctx context.Context,
	method string,
	req interface{},
	next func() (interface{}, error)) (rep interface{}, failed error) {

	// If the request is nil then pass control to the next handler
	// in the chain.
	if req == nil {
		return next()
	}

	w := &bytes.Buffer{}
	reqID, reqIDOK := csictx.GetRequestID(ctx)

	// Print the request
	if s.opts.reqw != nil {
		fmt.Fprintf(w, "%s: ", method)
		if reqIDOK {
			fmt.Fprintf(w, "REQ %04d", reqID)
		}
		s.rprintReqOrRep(w, req)
		fmt.Fprintln(s.opts.reqw, w.String())
	}

	w.Reset()

	// Get the response.
	rep, failed = next()

	if s.opts.repw == nil {
		return
	}

	// Print the response method name.
	fmt.Fprintf(w, "%s: ", method)
	if reqIDOK {
		fmt.Fprintf(w, "REP %04d", reqID)
	}

	// Print the response error if it is set.
	if failed != nil {
		fmt.Fprint(w, ": ")
		fmt.Fprint(w, failed)
	}

	// Print the response data if it is set.
	if !utils.IsNilResponse(rep) {
		s.rprintReqOrRep(w, rep)
	}
	fmt.Fprintln(s.opts.repw, w.String())

	return
}

var emptyValRX = regexp.MustCompile(
	`^((?:)|(?:\[\])|(?:<nil>)|(?:map\[\]))$`)

// rprintReqOrRep is used by the server-side interceptors that log
// requests and responses.
func (s *interceptor) rprintReqOrRep(w io.Writer, obj interface{}) {
	rv := reflect.ValueOf(obj).Elem()
	tv := rv.Type()
	nf := tv.NumField()
	printedColon := false
	printComma := false
	for i := 0; i < nf; i++ {
		name := tv.Field(i).Name
		if strings.Contains(name, "Secrets") {
			continue
		}
		if s.opts.disableLogVolCtx && strings.Contains(name, "VolumeContext") {
			continue
		}
		sv := fmt.Sprintf("%v", rv.Field(i).Interface())
		if emptyValRX.MatchString(sv) {
			continue
		}
		if printComma {
			fmt.Fprintf(w, ", ")
		}
		if !printedColon {
			fmt.Fprintf(w, ": ")
			printedColon = true
		}
		printComma = true
		fmt.Fprintf(w, "%s=%s", name, sv)
	}
}
