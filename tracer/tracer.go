package tracer

import (
	"fmt"
	"net/url"
	"strings"

	lightstep "github.com/lightstep/lightstep-tracer-go"
	opentracing "github.com/opentracing/opentracing-go"
	jaeger "github.com/uber/jaeger-client-go"
	"github.com/uber/jaeger-client-go/transport"
	"github.com/uber/jaeger-client-go/transport/udp"
	"github.com/uber/jaeger-client-go/transport/zipkin"
)

const (
	backendJaeger    = "jaeger"
	backendLightstep = "lightstep"
)

// NewTracer returns a new opentracing.Tracer via the options provided.
// If this isn't called the default tracer is opentracaing.NoopTracer{},
// resulting in no tracing being captured.
func NewTracer(opts Opts) (opentracing.Tracer, error) {
	switch opts.Backend {
	case backendJaeger:
		return newJaegerTracer(opts)
	case backendLightstep:
		return newLightstepTracer(opts)
	}

	return nil, fmt.Errorf("unknown tracer backend: '%s'", opts.Backend)
}

func newJaegerTracer(opts Opts) (opentracing.Tracer, error) {
	// Detect whether we're using zipkin-compatible tracing or jaeger's UDP
	// remote.
	u, err := url.Parse(opts.Addr)
	if err != nil {
		return nil, fmt.Errorf("error parsing tracing-addr: %s", err)
	}

	var trnsprt transport.Transport
	if strings.Contains(u.Scheme, "http") {
		trnsprt, err = zipkin.NewHTTPTransport(opts.Addr)
	} else {
		// no http or https, therefore we must be using jaeger's
		// reporter via UDP using its default max packet length.
		trnsprt, err = udp.NewUDPTransport(opts.Addr, 0)
	}

	if err != nil {
		return nil, fmt.Errorf("error configuring tracer: %s", err)
	}

	// jaeger has many samplers available, ranging from constant sampling
	// to guaranteed throughput probabilistic sampling. For ease of
	// configuration, by default use constant sampling.
	//
	// TODO(tonyhb): close the io.Closer provided by jaeger when a node is
	// terminated to drain the span queue
	tracer, _ := jaeger.NewTracer(opts.Hostname, jaeger.NewConstSampler(true), jaeger.NewRemoteReporter(trnsprt))
	return tracer, nil
}

func newLightstepTracer(opts Opts) (opentracing.Tracer, error) {
	return lightstep.NewTracer(lightstep.Options{
		AccessToken: opts.Addr,
	}), nil
}

// Opts hold all configuration opts for creating a new opentracing.Tracer via
// NewTracer
type Opts struct {
	// Backend is the type of tracer used, either jaeger or lightstep
	Backend string

	// Addr is used to reference either the zipkin backend URL for
	// collection; the jaeger UDP host and port for collection; or the
	// lightstep access token for collection
	//
	// For zipkin collection this should be something like
	// 'http://hostname:9411/api/v1/spans'. Note that 'http' or 'https'
	// is necessary; as is the API path.
	//
	// For jaeger UDP collection this should be something like
	// 'hostname:port'.
	Addr string

	// Hostname is used as the service name for jaeger tracing, and as a
	// global tag for lightstep tracing
	// TODO: potentially use nodeId instead of hostname
	Hostname string
}
