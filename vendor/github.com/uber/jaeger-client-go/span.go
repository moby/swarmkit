// Copyright (c) 2016 Uber Technologies, Inc.

// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN
// THE SOFTWARE.

package jaeger

import (
	"strings"
	"sync"
	"time"

	"github.com/opentracing/opentracing-go"
	"github.com/opentracing/opentracing-go/ext"
	"github.com/opentracing/opentracing-go/log"

	"github.com/uber/jaeger-client-go/utils"
)

type span struct {
	sync.RWMutex

	tracer *tracer

	context SpanContext

	// The name of the "operation" this span is an instance of.
	// Known as a "span name" in some implementations.
	operationName string

	// firstInProcess, if true, indicates that this span is the root of the (sub)tree
	// of spans in the current process. In other words it's true for the root spans,
	// and the ingress spans when the process joins another trace.
	firstInProcess bool

	// used to distinguish local vs. RPC Server vs. RPC Client spans
	spanKind string

	// startTime is the timestamp indicating when the span began, with microseconds precision.
	startTime time.Time

	// duration returns duration of the span with microseconds precision.
	// Zero value means duration is unknown.
	duration time.Duration

	// peer points to the peer service participating in this span,
	// e.g. the Client if this span is a server span,
	// or Server if this span is a client span
	peer struct {
		Ipv4        int32
		Port        int16
		ServiceName string
	}

	// tags attached to this span
	tags []Tag

	// The span's "micro-log"
	logs []opentracing.LogRecord
}

// Tag a simple key value wrapper
type Tag struct {
	key   string
	value interface{}
}

// Sets or changes the operation name.
func (s *span) SetOperationName(operationName string) opentracing.Span {
	s.Lock()
	defer s.Unlock()
	if s.context.IsSampled() {
		s.operationName = operationName
	}
	return s
}

// SetTag implements SetTag() of opentracing.Span
func (s *span) SetTag(key string, value interface{}) opentracing.Span {
	if key == string(ext.SamplingPriority) && setSamplingPriority(s, key, value) {
		return s
	}
	s.Lock()
	defer s.Unlock()
	if s.context.IsSampled() {
		s.setTagNoLocking(key, value)
	}
	return s
}

func (s *span) setTagNoLocking(key string, value interface{}) {
	handled := false
	if handler, ok := specialTagHandlers[key]; ok {
		handled = handler(s, key, value)
	}
	if !handled {
		s.tags = append(s.tags, Tag{key: key, value: value})
	}
}

func (s *span) setTracerTags(tags []Tag) {
	s.Lock()
	for _, tag := range tags {
		s.tags = append(s.tags, tag)
	}
	s.Unlock()
}

func (s *span) LogFields(fields ...log.Field) {
	s.Lock()
	defer s.Unlock()
	if !s.context.IsSampled() {
		return
	}
	lr := opentracing.LogRecord{
		Fields:    fields,
		Timestamp: time.Now(),
	}
	s.appendLog(lr)
}

func (s *span) LogKV(alternatingKeyValues ...interface{}) {
	s.RLock()
	sampled := s.context.IsSampled()
	s.RUnlock()
	if !sampled {
		return
	}
	fields, err := log.InterleavedKVToFields(alternatingKeyValues...)
	if err != nil {
		s.LogFields(log.Error(err), log.String("function", "LogKV"))
		return
	}
	s.LogFields(fields...)
}

func (s *span) LogEvent(event string) {
	s.Log(opentracing.LogData{Event: event})
}

func (s *span) LogEventWithPayload(event string, payload interface{}) {
	s.Log(opentracing.LogData{Event: event, Payload: payload})
}

func (s *span) Log(ld opentracing.LogData) {
	s.Lock()
	defer s.Unlock()
	if s.context.IsSampled() {
		if ld.Timestamp.IsZero() {
			ld.Timestamp = s.tracer.timeNow()
		}
		s.appendLog(ld.ToLogRecord())
	}
}

// this function should only be called while holding a Write lock
func (s *span) appendLog(lr opentracing.LogRecord) {
	// TODO add logic to limit number of logs per span (issue #46)
	s.logs = append(s.logs, lr)
}

// SetBaggageItem implements SetBaggageItem() of opentracing.SpanContext
func (s *span) SetBaggageItem(key, value string) opentracing.Span {
	key = normalizeBaggageKey(key)
	s.Lock()
	defer s.Unlock()
	s.context = s.context.WithBaggageItem(key, value)
	return s
}

// BaggageItem implements BaggageItem() of opentracing.SpanContext
func (s *span) BaggageItem(key string) string {
	key = normalizeBaggageKey(key)
	s.RLock()
	defer s.RUnlock()
	return s.context.baggage[key]
}

func (s *span) Finish() {
	s.FinishWithOptions(opentracing.FinishOptions{})
}

func (s *span) FinishWithOptions(options opentracing.FinishOptions) {
	s.Lock()
	if s.context.IsSampled() {
		finishTime := options.FinishTime
		if finishTime.IsZero() {
			finishTime = s.tracer.timeNow()
		}
		s.duration = finishTime.Sub(s.startTime)
		// Note: bulk logs are not subject to maxLogsPerSpan limit
		if options.LogRecords != nil {
			s.logs = append(s.logs, options.LogRecords...)
		}
		for _, ld := range options.BulkLogData {
			s.logs = append(s.logs, ld.ToLogRecord())
		}
	}
	s.Unlock()
	// call reportSpan even for non-sampled traces, to return span to the pool
	s.tracer.reportSpan(s)
}

func (s *span) Context() opentracing.SpanContext {
	return s.context
}

func (s *span) Tracer() opentracing.Tracer {
	return s.tracer
}

func (s *span) String() string {
	return s.context.String()
}

func (s *span) peerDefined() bool {
	return s.peer.ServiceName != "" || s.peer.Ipv4 != 0 || s.peer.Port != 0
}

func (s *span) isRPC() bool {
	s.RLock()
	defer s.RUnlock()
	return s.spanKind == string(ext.SpanKindRPCClientEnum) || s.spanKind == string(ext.SpanKindRPCServerEnum)
}

func (s *span) isRPCClient() bool {
	s.RLock()
	defer s.RUnlock()
	return s.spanKind == string(ext.SpanKindRPCClientEnum)
}

var specialTagHandlers = map[string]func(*span, string, interface{}) bool{
	string(ext.SpanKind):     setSpanKind,
	string(ext.PeerHostIPv4): setPeerIPv4,
	string(ext.PeerPort):     setPeerPort,
	string(ext.PeerService):  setPeerService,
}

func setSpanKind(s *span, key string, value interface{}) bool {
	if val, ok := value.(string); ok {
		s.spanKind = val
		return true
	}
	if val, ok := value.(ext.SpanKindEnum); ok {
		s.spanKind = string(val)
		return true
	}
	return false
}

func setPeerIPv4(s *span, key string, value interface{}) bool {
	if val, ok := value.(string); ok {
		if ip, err := utils.ParseIPToUint32(val); err == nil {
			s.peer.Ipv4 = int32(ip)
			return true
		}
	}
	if val, ok := value.(uint32); ok {
		s.peer.Ipv4 = int32(val)
		return true
	}
	if val, ok := value.(int32); ok {
		s.peer.Ipv4 = val
		return true
	}
	return false
}

func setPeerPort(s *span, key string, value interface{}) bool {
	if val, ok := value.(string); ok {
		if port, err := utils.ParsePort(val); err == nil {
			s.peer.Port = int16(port)
			return true
		}
	}
	if val, ok := value.(uint16); ok {
		s.peer.Port = int16(val)
		return true
	}
	if val, ok := value.(int); ok {
		s.peer.Port = int16(val)
		return true
	}
	return false
}

func setPeerService(s *span, key string, value interface{}) bool {
	if val, ok := value.(string); ok {
		s.peer.ServiceName = val
		return true
	}
	return false
}

func setSamplingPriority(s *span, key string, value interface{}) bool {
	s.Lock()
	defer s.Unlock()
	if val, ok := value.(uint16); ok {
		if val > 0 {
			s.context.flags = s.context.flags | flagDebug | flagSampled
		} else {
			s.context.flags = s.context.flags & (^flagSampled)
		}
		return true
	}
	return false
}

// Converts end-user baggage key into internal representation.
// Used for both read and write access to baggage items.
func normalizeBaggageKey(key string) string {
	// TODO(yurishkuro) normalizeBaggageKey: cache the results in some bounded LRU cache
	return strings.Replace(strings.ToLower(key), "_", "-", -1)
}
