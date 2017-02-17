package tracer

import (
	"encoding/json"

	"github.com/docker/swarmkit/api"
	"github.com/docker/swarmkit/log"
	opentracing "github.com/opentracing/opentracing-go"
	"golang.org/x/net/context"
)

// Marshal encodes an opentracing.Span's contextual information into a binary
// encoded string in the same way that spans can be sent via http headers
func Marshal(span opentracing.Span) (string, error) {
	carrier := opentracing.TextMapCarrier{}
	opentracing.GlobalTracer().Inject(
		span.Context(),
		opentracing.TextMap,
		carrier,
	)

	// Now that we have our text map JSON encode it so that we can add it
	// as a single annotation to a service
	data, err := json.Marshal(carrier)
	// TODO(tonyhb): do we use error wrapping like
	// palantir/stacktrace or pkg/errors?
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// StartSpanFromTask is a helper for extracting an encoded span from a task's
// ServiceAnnotation, checking for nil along the way to prevent panics.
// This delegates to Unmarshal once the encoded data has been extracted.
func StartSpanFromTask(ctx context.Context, operationName string, t *api.Task) (opentracing.Span, context.Context) {
	if t.ServiceAnnotations.Labels == nil {
		log.GetLogger(ctx).Debug("task has no tracing annotation")
		span := opentracing.StartSpan(operationName)
		return span, opentracing.ContextWithSpan(ctx, span)
	}
	enc, ok := t.ServiceAnnotations.Labels["span"]
	if !ok {
		log.GetLogger(ctx).Debug("task has no tracing annotation")
		span := opentracing.StartSpan(operationName)
		return span, opentracing.ContextWithSpan(ctx, span)
	}
	span := Unmarshal(operationName, enc)
	return span, opentracing.ContextWithSpan(ctx, span)
}

// Unmarshal takes a previously encoded span context and returns a new span
// for the given operation name.
//
// If there's an error creating a span from the given marshalled span parent
// a new span is created.
func Unmarshal(operationName, enc string) opentracing.Span {
	carrier := opentracing.TextMapCarrier{}
	err := json.Unmarshal([]byte(enc), &carrier)
	if err != nil {
		return opentracing.StartSpan(operationName)
	}

	sc, err := opentracing.GlobalTracer().Extract(
		opentracing.TextMap,
		opentracing.TextMapCarrier(carrier),
	)
	if err != nil {
		return opentracing.StartSpan(operationName)
	}

	// Create a SpanReference which is passed into StartSpan, creating a
	// new span which retains a reference to its parent.
	ref := opentracing.ChildOf(sc)
	span := opentracing.StartSpan(operationName, ref)
	return span
}
