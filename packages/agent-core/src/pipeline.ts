// Agent pipeline (SPEC §5.1 steps 0-7). The application owns sequencing,
// validation, budgets and state transitions; the LLM only proposes structured
// classification/draft content. There is NO model-driven execute step.
// The store handle used here must be an "agent"-role handle: it structurally
// cannot write tickets/outbox/approvals/execution_jobs.
import { BenriStore, contentHash, newId} from "@benri/db";
import { LexicalIndex, type SearchHit} from "@benri/retrieval";
import { Tracer, newTraceId, type Span} from "@benri/observability";
import { GatewayError, type ModelRailClient} from "@benri/modelrail-client";
import { redactPII, maskOrderNo} from "./redact.js";
import { evaluatePolicy} from "./policy.js";
import { verifyDraft, type VerificationResult} from "./verifier.js";
import { actionTypesForIntent, buildCanonicalPayload, payloadHash} from "./actions.js";
import type { DraftOutput, EvidenceItem, OrderContext} from "./mock-llm.js";
import { BenriError, PROMPT_VERSION, POLICY_VERSION, type BenriLimits, type EscalationReason} from "./taxonomy.js";

export interface ToolGateway {
searchKb(tenantId: string, query: string, intent: string, limit: number): Promise<SearchHit[]>;
lookupOrder(tenantId: string, orderNo: string): Promise<Record<string, unknown> | null>;
}

export class DirectToolGateway implements ToolGateway {
constructor(private store: BenriStore) {}
async searchKb(tenantId: string, query: string, intent: string, limit: number): Promise<SearchHit[]> {
const articles = this.store.listActiveArticles(tenantId);
const index = new LexicalIndex(articles.map(a => ({
article_id: String(a.id), chunk_id: `${a.id}-c0`, title: String(a.title), text: String(a.body),
intent_tags: JSON.parse(String(a.intent_tags)) as string[],
})));
return index.search(query, { intent, limit});
}
async lookupOrder(tenantId: string, orderNo: string) {
const row = this.store.findOrderByNo(tenantId, orderNo);
return row? (row as unknown as Record<string, unknown>): null;
}
}

export interface PipelineHooks {
/** Test-only: throw after checkpointing the named step (crash simulation). */
crashAfterStep?: string;
/** Test-only: disable retrieval (negative control §11.5). */
disableRetrieval?: boolean;
/** Test-only: malformed tool output simulation. */
malformedToolOutput?: boolean;
}
export class CrashSimulatedError extends Error { code = "CRASH_SIMULATED";}

export interface PipelineResult {
run_id: string;
outcome: "awaiting_review" | "escalated" | "failed";
escalation_reason?: EscalationReason;
failure_code?: string;
draft_id?: string;
trace_id: string;
}

interface StepOutputs {
classification?: { intent: string; urgency: string; confidence: number};
hits?: SearchHit[];
order?: OrderContext | null;
orderFound?: boolean;
}

export async function runPipeline(opts: {
store: BenriStore; runId: string; tools: ToolGateway; llm: ModelRailClient;
limits: BenriLimits; tracer?: Tracer; hooks?: PipelineHooks;
}): Promise<PipelineResult> {
const { store, runId, tools, llm, limits} = opts;
const tracer = opts.tracer?? new Tracer();
const run = store.getRunById(runId);
if (!run) throw new BenriError("RUN_NOT_FOUND", `run ${runId}`);
const tenantId = String(run.tenant_id);
const inquiry = store.getInquiry(tenantId, String(run.inquiry_id));
if (!inquiry) throw new BenriError("INQUIRY_NOT_FOUND", "inquiry missing for run");
const traceId = String(run.trace_id?? newTraceId());
const rootSpan = tracer.startSpan(traceId, "benri.run", undefined, { "benri.run_id": runId});
const startedAt = Date.now();
let modelCalls = 0; let toolCalls = 0;
const usage = { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0};

const done = store.completedStepKeys(runId);
const outputs: StepOutputs = {};
// Restore outputs from checkpoints when resuming.
for (const cp of store.getCheckpoints(tenantId, runId)) {
if (cp.state!== "completed" ||!cp.output_redacted) continue;
const out = JSON.parse(String(cp.output_redacted)) as Record<string, unknown>;
if (cp.step_key === "classify") outputs.classification = out as StepOutputs["classification"];
if (cp.step_key === "retrieve") outputs.hits = (out as { hits: SearchHit[]}).hits;
if (cp.step_key === "lookup_order") { outputs.order = (out as { order: OrderContext | null}).order; outputs.orderFound = (out as { found: boolean}).found;}
}

const checkpoint = (stepKey: string, output: unknown, span: Span, t0: number, errorCode?: string) => {
store.saveCheckpoint({
tenant_id: tenantId, run_id: runId, step_key: stepKey, attempt: 1,
state: errorCode? "failed": "completed", output_redacted: output?? undefined,
prompt_version: PROMPT_VERSION, model: llm.mock? "mock-deterministic-v1": "modelrail",
duration_ms: Date.now() - t0, trace_id: traceId, span_id: span.span_id, error_code: errorCode?? null,
});
if (opts.hooks?.crashAfterStep === stepKey) throw new CrashSimulatedError(`simulated crash after ${stepKey}`);
};
const checkLimits = () => {
if (modelCalls > limits.maxModelCallsPerRun) throw new BenriError("MODEL_BUDGET_EXCEEDED", "model call budget exceeded");
if (toolCalls > limits.maxToolCallsPerRun) throw new BenriError("STEP_LIMIT_EXCEEDED", "tool call budget exceeded");
if (Date.now() - startedAt > limits.maxRunDurationMs) throw new BenriError("RUN_TIMEOUT", "run duration exceeded");
};
const recordTool = (server: string, tool: string, risk: string, args: unknown, status: string, latency: number, spanId: string) => {
store.insertToolCall({ tenant_id: tenantId, run_id: runId, server, tool, risk_level: risk, args_redacted: args, status, latency_ms: latency, trace_id: traceId, span_id: spanId});
};

const escalate = (reason: EscalationReason, noteJp: string): PipelineResult => {
const summary = `【内部引き継ぎサマリー】理由: ${reason}\n${noteJp}\n対象カテゴリ: ${outputs.classification?.intent?? "未判定"}\n確認した根拠: ${(outputs.hits?? []).map(h => h.title).join("、") || "なし"}\n問い合わせ概要(マスキング済み): ${String(inquiry.redacted_body).slice(0, 120)}`;
store.finishRun(runId, { state: "escalated", escalation_reason: reason, handoff_summary: summary, usage_json: usage, trace_id: traceId});
store.updateInquiryStatus(tenantId, String(inquiry.id), "escalated");
store.insertAudit({ tenant_id: tenantId, actor_type: "agent", actor_id: "agent-worker", action: "run.escalated", entity_type: "run", entity_id: runId, after_hash: contentHash(reason), trace_id: traceId});
tracer.endSpan(rootSpan, "ok");
return { run_id: runId, outcome: "escalated", escalation_reason: reason, trace_id: traceId};
};
const fail = (code: string): PipelineResult => {
store.finishRun(runId, { state: "failed", failure_code: code, usage_json: usage, trace_id: traceId});
store.updateInquiryStatus(tenantId, String(inquiry.id), "failed");
tracer.endSpan(rootSpan, "error");
return { run_id: runId, outcome: "failed", failure_code: code, trace_id: traceId};
};

try {
store.updateInquiryStatus(tenantId, String(inquiry.id), "processing");

// Step 0 — normalize/redact (idempotent; body was redacted at ingestion,
// re-apply for defence in depth on the stored redacted body).
if (!done.has("normalize_redact")) {
const t0 = Date.now();
const { span} = await tracer.withSpan(traceId, "normalize_redact", rootSpan.span_id, {}, async () => {
const r = redactPII(String(inquiry.redacted_body));
return r.text;
});
checkpoint("normalize_redact", { redacted: true}, span, t0);
}
const text = redactPII(String(inquiry.redacted_body)).text;

// Step 1 — classify (LLM + schema)
if (!done.has("classify")) {
const t0 = Date.now();
const { result, span} = await tracer.withSpan(traceId, "classify", rootSpan.span_id, {}, async () => {
modelCalls++; checkLimits();
const res = await llm.chat([
{ role: "system", content: "TASK:CLASSIFY — return JSON {intent, urgency, confidence} using the fixed Benri taxonomy."},
{ role: "user", content: JSON.stringify({ text})},
]);
usage.prompt_tokens += res.usage.prompt_tokens; usage.completion_tokens += res.usage.completion_tokens; usage.total_tokens += res.usage.total_tokens;
return JSON.parse(res.content) as StepOutputs["classification"];
});
outputs.classification = result;
checkpoint("classify", result, span, t0);
}
const classification = outputs.classification!;
const intent = classification.intent;

// Step 2 — retrieve KB evidence (read-only MCP/tool gateway)
if (!done.has("retrieve")) {
const t0 = Date.now();
const { result, span} = await tracer.withSpan(traceId, "retrieve", rootSpan.span_id, {}, async () => {
toolCalls++; checkLimits();
const t = Date.now();
if (opts.hooks?.disableRetrieval) { recordTool("kb-mcp", "search_kb", "read", { intent}, "ok", 0, ""); return [] as SearchHit[];}
const hits = await tools.searchKb(tenantId, `${inquiry.subject} ${text}`, intent, 5);
recordTool("kb-mcp", "search_kb", "read", { intent, limit: 5}, "ok", Date.now() - t, "");
return hits;
});
outputs.hits = result;
checkpoint("retrieve", { hits: result}, span, t0);
}
const hits = outputs.hits?? [];

// Step 3 — order lookup (read-only) when a validated hint is present
const orderHint = inquiry.order_no_hint? String(inquiry.order_no_hint): null;
if (!done.has("lookup_order")) {
const t0 = Date.now();
const { result, span} = await tracer.withSpan(traceId, "lookup_order", rootSpan.span_id, {}, async () => {
if (!orderHint) return { order: null as OrderContext | null, found: false};
toolCalls++; checkLimits();
const t = Date.now();
const row = await tools.lookupOrder(tenantId, orderHint);
recordTool("crm-mcp", "lookup_order", "read", { order_no: maskOrderNo(orderHint)}, row? "ok": "not_found", Date.now() - t, "");
if (opts.hooks?.malformedToolOutput && row) return { order: null as OrderContext | null, found: false, malformed: true};
const order: OrderContext | null = row? {
order_no_masked: maskOrderNo(orderHint),
item: String(row.item), delivery_status: String(row.delivery_status), ordered_at: String(row.ordered_at),
}: null;
return { order, found:!!row};
});
outputs.order = result.order; outputs.orderFound = result.found;
checkpoint("lookup_order", result, span, t0);
}
const order = outputs.order?? null;

// Step 4 — deterministic policy
let decision;
{
const t0 = Date.now();
const { result, span} = await tracer.withSpan(traceId, "policy_decision", rootSpan.span_id, {}, async () =>
evaluatePolicy({
text, classification: { intent: intent as never, urgency: classification.urgency as never, confidence: classification.confidence, matched: []},
evidenceCount: hits.length, orderRequired: false, orderFound:!!outputs.orderFound, orderHintPresent:!!orderHint,
orderAmbiguous: (text.match(/\[注文番号\]/g) ?? []).length >= 2 || text.includes("どちらか分かりません"),
          evidenceTexts: hits.map(h => { const a = store.getArticle(tenantId, h.article_id); return a ? String(a.body) : h.excerpt; }),
}));
decision = result;
if (!done.has("policy_decision")) checkpoint("policy_decision", decision, span, t0);
}
if (decision.escalate) return escalate(decision.reason!, decision.note_jp?? "");

// Evidence bundle for drafting/verification
const evidence: EvidenceItem[] = hits.map(h => {
const art = store.getArticle(tenantId, h.article_id);
return { article_id: h.article_id, chunk_id: h.chunk_id, title: h.title, text: art? String(art.body): h.excerpt};
});
const articleTenant = new Map<string, string>();
for (const a of store.listArticles(tenantId)) articleTenant.set(String(a.id), tenantId);
const orderText = order? `${order.item} ${order.delivery_status}`: "";

// Step 5 — draft (LLM structured output; no actions, no tool args)
let draft: DraftOutput;
{
const t0 = Date.now();
const { result, span} = await tracer.withSpan(traceId, "draft", rootSpan.span_id, {}, async () => {
modelCalls++; checkLimits();
const res = await llm.chat([
{ role: "system", content: "TASK:DRAFT — return JSON DraftOutput {body, citedArticleIds, factualReferences} grounded only in the evidence bundle."},
{ role: "user", content: JSON.stringify({ intent, evidence, order, query: `${inquiry.subject} ${text}` }) },
]);
usage.prompt_tokens += res.usage.prompt_tokens; usage.completion_tokens += res.usage.completion_tokens; usage.total_tokens += res.usage.total_tokens;
return JSON.parse(res.content) as DraftOutput;
});
draft = result;
if (!done.has("draft")) checkpoint("draft", { cited: draft.citedArticleIds}, span, t0);
}

// Step 6 — verify (one bounded repair attempt, then escalate)
let verification: VerificationResult = verifyDraft(draft, { tenantId, evidence, articleTenant, order, orderText});
if (!verification.passed) {
// repair: rebuild the draft from evidence only (drops unsupported content)
const { buildDraft} = await import("./mock-llm.js");
draft = buildDraft(intent, evidence, order, `${inquiry.subject} ${text}`);
verification = verifyDraft(draft, { tenantId, evidence, articleTenant, order, orderText});
}
{
const t0 = Date.now();
const { span} = await tracer.withSpan(traceId, "verify", rootSpan.span_id, {}, async () => verification);
if (!done.has("verify")) checkpoint("verify", { passed: verification.passed}, span, t0);
}
if (!verification.passed) return escalate("VERIFICATION_FAILED", "ドラフトの検証に失敗したため、担当者へ引き継ぎます。");

// Step 7 — persist draft + deterministic proposed actions; stop at review.
const revision = store.nextRevision(runId);
const draftId = newId("draft");
const contentHashValue = contentHash(draft.body);
store.insertDraft({
id: draftId, tenant_id: tenantId, run_id: runId, revision, body: draft.body,
cited_article_ids: draft.citedArticleIds, factual_references: draft.factualReferences,
verification_json: verification, content_hash: contentHashValue,
});
for (const actionType of actionTypesForIntent(intent as never)) {
const payload = buildCanonicalPayload({
tenant_id: tenantId, inquiry_id: String(inquiry.id), draft_id: draftId, draft_revision: revision,
action_type: actionType, draft_body: draft.body, subject: String(inquiry.subject), urgency: classification.urgency,
});
store.insertProposedAction({
id: newId("action"), tenant_id: tenantId, draft_id: draftId, run_id: runId,
action_type: actionType, canonical_payload: payload, payload_hash: payloadHash(payload),
});
}
store.finishRun(runId, { state: "completed", usage_json: usage, trace_id: traceId});
store.updateInquiryStatus(tenantId, String(inquiry.id), "needs_review");
store.insertAudit({ tenant_id: tenantId, actor_type: "agent", actor_id: "agent-worker", action: "run.completed_awaiting_review", entity_type: "run", entity_id: runId, after_hash: contentHashValue, trace_id: traceId});
tracer.endSpan(rootSpan, "ok");
return { run_id: runId, outcome: "awaiting_review", draft_id: draftId, trace_id: traceId};
} catch (e) {
if (e instanceof CrashSimulatedError) throw e; // worker crash: leave run 'running' with lease for resume
if (e instanceof GatewayError) return fail("GATEWAY_UNAVAILABLE");
if (e instanceof BenriError && e.code === "MODEL_BUDGET_EXCEEDED") return escalate("MODEL_BUDGET_EXCEEDED", "モデル呼び出し上限に達したため、担当者へ引き継ぎます。");
if (e instanceof BenriError) return fail(e.code);
return fail("INTERNAL_ERROR");
}
}

export { POLICY_VERSION};
