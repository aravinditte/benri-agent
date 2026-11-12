// OTel-shaped tracing (SPEC §10.2). Spans follow the OpenTelemetry data model
// (trace_id/span_id/parent, name, attributes, status) and are collected
// in-memory for tests/UI; when KAIRO_OTLP_ENDPOINT is set they are also
// exported asynchronously as OTLP/JSON. Export failure NEVER fails a run.
// Span attributes never contain raw inquiry text, PII, or order numbers.
import { randomBytes } from "node:crypto";

export interface Span {
  trace_id: string; span_id: string; parent_span_id?: string;
  name: string; started_at: string; ended_at?: string;
  attributes: Record<string, string | number | boolean>;
  status: "ok" | "error";
}
const hex = (n: number) => randomBytes(n).toString("hex");
export const newTraceId = () => hex(16);
export const newSpanId = () => hex(8);

export class SpanCollector {
  spans: Span[] = [];
  add(s: Span) { this.spans.push(s); }
  forTrace(traceId: string) { return this.spans.filter(s => s.trace_id === traceId); }
  clear() { this.spans = []; }
}
export const globalCollector = new SpanCollector();

export class Tracer {
  constructor(private collector: SpanCollector = globalCollector, private otlpEndpoint?: string) {}
  startSpan(traceId: string, name: string, parentSpanId?: string, attributes: Span["attributes"] = {}): Span {
    const span: Span = { trace_id: traceId, span_id: newSpanId(), parent_span_id: parentSpanId, name, started_at: new Date().toISOString(), attributes, status: "ok" };
    return span;
  }
  endSpan(span: Span, status: "ok" | "error" = "ok") {
    span.ended_at = new Date().toISOString();
    span.status = status;
    this.collector.add(span);
    if (this.otlpEndpoint) void this.exportOtlp(span);
  }
  async withSpan<T>(traceId: string, name: string, parentSpanId: string | undefined, attributes: Span["attributes"], fn: () => Promise<T>): Promise<{ result: T; span: Span }> {
    const span = this.startSpan(traceId, name, parentSpanId, attributes);
    try { const result = await fn(); this.endSpan(span, "ok"); return { result, span }; }
    catch (e) { this.endSpan(span, "error"); throw e; }
  }
  private async exportOtlp(span: Span): Promise<void> {
    try {
      const body = { resourceSpans: [{ resource: { attributes: [{ key: "service.name", value: { stringValue: "benri" } }] }, scopeSpans: [{ spans: [{ traceId: span.trace_id, spanId: span.span_id, parentSpanId: span.parent_span_id ?? "", name: span.name, kind: 1, startTimeUnixNano: String(Date.parse(span.started_at) * 1e6), endTimeUnixNano: String(Date.parse(span.ended_at ?? span.started_at) * 1e6), attributes: Object.entries(span.attributes).map(([key, v]) => ({ key, value: typeof v === "number" ? { intValue: String(v) } : { stringValue: String(v) } })) }] }] }] };
      await fetch(`${this.otlpEndpoint!.replace(/\/$/, "")}/v1/traces`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body), signal: AbortSignal.timeout(2000) });
    } catch { /* exporter down must not fail runs (SPEC §10.2) */ }
  }
}
