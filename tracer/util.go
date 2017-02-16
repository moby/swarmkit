package tracer

import opentracing "github.com/opentracing/opentracing-go"

// SetError sets all three error tracking tags on the opentracing Span as
// specified by https://github.com/opentracing/specification/blob/85d47cc/data_conventions.yaml
func SetError(span opentracing.Span, err error) {
	span.SetTag("error", true)
	span.SetTag("event", "error")
	span.SetTag("message", err.Error())
}
