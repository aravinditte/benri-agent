// Package observability sets up OpenTelemetry (SPEC §10.2): one root span
// per run (benri.run) with child spans per step/tool, W3C trace context,
// and asynchronous OTLP/HTTP export to Kairo when configured. Export failure
// NEVER fails a run. A local in-memory span recorder is always installed so
// tests and local debugging capture traces even with the exporter disabled.
// Span attributes never contain raw inquiry text, PII or order numbers.
package observability

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// SpanRecord is a finished span captured locally.
type SpanRecord struct {
	TraceID  string
	SpanID   string
	ParentID string
	Name     string
	Status   string
}

type Recorder struct {
	mu    sync.Mutex
	spans []SpanRecord
}

func (r *Recorder) OnEnd(s sdktrace.ReadOnlySpan) {
	status := "ok"
	if s.Status().Code.String() == "Error" {
		status = "error"
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = append(r.spans, SpanRecord{TraceID: s.SpanContext().TraceID().String(), SpanID: s.SpanContext().SpanID().String(), ParentID: s.Parent().SpanID().String(), Name: s.Name(), Status: status})
}
func (r *Recorder) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (r *Recorder) Shutdown(context.Context) error                  { return nil }
func (r *Recorder) ForceFlush(context.Context) error                { return nil }
func (r *Recorder) ForTrace(traceID string) []SpanRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []SpanRecord{}
	for _, s := range r.spans {
		if s.TraceID == traceID {
			out = append(out, s)
		}
	}
	return out
}
func (r *Recorder) All() []SpanRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]SpanRecord{}, r.spans...)
}
func (r *Recorder) Clear() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.spans = nil
}

var GlobalRecorder = &Recorder{}

// Setup installs the tracer provider. otlpEndpoint may be empty (local
// capture only). It returns a shutdown function; exporter errors are
// swallowed by the SDK's async batch processor and never block runs.
func Setup(ctx context.Context, serviceName, otlpEndpoint string) (func(context.Context) error, error) {
	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(GlobalRecorder),
		sdktrace.WithResource(resource.NewWithAttributes(semconv.SchemaURL, semconv.ServiceName(serviceName))),
	}
	if otlpEndpoint != "" {
		exp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(otlpEndpoint), otlptracehttp.WithInsecure())
		if err == nil {
			opts = append(opts, sdktrace.WithBatcher(exp))
		}
	}
	tp := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	return tp.Shutdown, nil
}

func Tracer() trace.Tracer { return otel.Tracer("benri") }

// NewTraceID returns a random W3C-format trace id (32 hex chars).
func NewTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
