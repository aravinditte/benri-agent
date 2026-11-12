package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/observability"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/verify"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ToolGateway abstracts the read-only tool surface. The agent worker hosts
// only read-only KB/CRM gateways (direct or MCP); it has NO route to the
// ticket MCP server or the executor (SPEC §3.3, §7).
type ToolGateway interface {
	SearchKB(ctx context.Context, tenantID, query, intent string, limit int) ([]retrieval.SearchHit, error)
	LookupOrder(ctx context.Context, tenantID, orderNo string) (*store.Order, error)
}

// DirectToolGateway calls the store/retrieval directly (tests + eval).
type DirectToolGateway struct {
	Store store.Repository
	TK    japanese.Tokenizer
}

func (g DirectToolGateway) SearchKB(_ context.Context, tenantID, query, intent string, limit int) ([]retrieval.SearchHit, error) {
	articles, err := g.Store.ListActiveArticles(tenantID)
	if err != nil {
		return nil, err
	}
	idx := retrieval.NewIndex(retrieval.DocsFromArticles(articles), g.TK)
	return idx.Search(query, intent, limit), nil
}
func (g DirectToolGateway) LookupOrder(_ context.Context, tenantID, orderNo string) (*store.Order, error) {
	return g.Store.FindOrderByNo(tenantID, orderNo)
}

type Hooks struct {
	CrashAfterStep      string
	DisableRetrieval    bool
	MalformedToolOutput bool
}

type CrashSimulatedError struct{ Step string }

func (e *CrashSimulatedError) Error() string { return "CRASH_SIMULATED after " + e.Step }

type Result struct {
	RunID            string `json:"run_id"`
	Outcome          string `json:"outcome"` // awaiting_review | escalated | failed
	EscalationReason string `json:"escalation_reason,omitempty"`
	FailureCode      string `json:"failure_code,omitempty"`
	DraftID          string `json:"draft_id,omitempty"`
	TraceID          string `json:"trace_id"`
}

type stepOutputs struct {
	Classification *Classification
	Hits           []retrieval.SearchHit
	Order          *OrderContext
	OrderFound     bool
}

// RunPipeline executes SPEC §5.1 steps 0–7 for a claimed run. The store
// handle must be an "agent"-role handle: it structurally cannot approve,
// enqueue execution jobs, or write tickets/outbox.
func RunPipeline(ctx context.Context, st store.Repository, runID string, tools ToolGateway, llm *modelrail.Client, limits domain.Limits, hooks *Hooks) (*Result, error) {
	if hooks == nil {
		hooks = &Hooks{}
	}
	run, err := st.GetRunByID(runID)
	if err != nil || run == nil {
		return nil, domain.NewError("RUN_NOT_FOUND", "run %s", runID)
	}
	tenantID := run.TenantID
	inquiry, err := st.GetInquiry(tenantID, run.InquiryID)
	if err != nil || inquiry == nil {
		return nil, domain.NewError("INQUIRY_NOT_FOUND", "inquiry missing for run")
	}
	traceID := observability.NewTraceID()
	if run.TraceID != nil && *run.TraceID != "" {
		traceID = *run.TraceID
	}
	tracer := observability.Tracer()
	ctx, rootSpan := tracer.Start(ctx, "benri.run", oteltrace.WithAttributes(attribute.String("benri.run_id", runID)))
	defer rootSpan.End()
	// Re-key the recorded trace id to the OTel span's trace id for UI links.
	traceID = rootSpan.SpanContext().TraceID().String()

	startedAt := time.Now()
	modelCalls, toolCalls := 0, 0
	usage := modelrail.Usage{}

	done, err := st.CompletedStepKeys(runID)
	if err != nil {
		return nil, err
	}
	outputs := stepOutputs{}
	cps, _ := st.GetCheckpoints(tenantID, runID)
	for _, cp := range cps {
		if cp.State != "completed" || cp.OutputRedacted == nil {
			continue
		}
		switch cp.StepKey {
		case "classify":
			var c Classification
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &c)
			outputs.Classification = &c
		case "retrieve":
			var wrap struct {
				Hits []retrieval.SearchHit `json:"hits"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			outputs.Hits = wrap.Hits
		case "lookup_order":
			var wrap struct {
				Order *OrderContext `json:"order"`
				Found bool          `json:"found"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			outputs.Order, outputs.OrderFound = wrap.Order, wrap.Found
		}
	}

	usageJSON := func() *string {
		b, _ := json.Marshal(map[string]int{"prompt_tokens": usage.PromptTokens, "completion_tokens": usage.CompletionTokens, "total_tokens": usage.TotalTokens})
		s := string(b)
		return &s
	}
	checkpoint := func(stepKey string, output any, span oteltrace.Span, t0 time.Time, errCode *string) error {
		var outStr *string
		if output != nil {
			b, _ := json.Marshal(output)
			s := string(b)
			outStr = &s
		}
		state := "completed"
		if errCode != nil {
			state = "failed"
		}
		dur := time.Since(t0).Milliseconds()
		pv := domain.PromptVersion
		model := "modelrail"
		if llm.Mock() {
			model = "mock-deterministic-v1"
		}
		tid, sid := traceID, span.SpanContext().SpanID().String()
		if err := st.SaveCheckpoint(store.CheckpointInput{TenantID: tenantID, RunID: runID, StepKey: stepKey, Attempt: 1, State: state, OutputRedacted: outStr, PromptVersion: &pv, Model: &model, DurationMS: &dur, TraceID: &tid, SpanID: &sid, ErrorCode: errCode}); err != nil {
			return err
		}
		if hooks.CrashAfterStep == stepKey {
			return &CrashSimulatedError{Step: stepKey}
		}
		return nil
	}
	checkLimits := func() error {
		if modelCalls > limits.MaxModelCallsPerRun {
			return domain.NewError(domain.EscBudgetExceeded, "model call budget exceeded")
		}
		if toolCalls > limits.MaxToolCallsPerRun {
			return domain.NewError("STEP_LIMIT_EXCEEDED", "tool call budget exceeded")
		}
		if time.Since(startedAt).Milliseconds() > int64(limits.MaxRunDurationMS) {
			return domain.NewError("RUN_TIMEOUT", "run duration exceeded")
		}
		return nil
	}
	recordTool := func(server, tool, risk string, args any, status string, latencyMS int64, spanID string) {
		argsJSON, _ := json.Marshal(args)
		s := string(argsJSON)
		rid := runID
		tid, sid := traceID, spanID
		_, _ = st.InsertToolCall(store.ToolCall{TenantID: tenantID, RunID: &rid, Server: server, Tool: tool, RiskLevel: risk, ArgsRedacted: &s, Status: status, LatencyMS: &latencyMS, TraceID: &tid, SpanID: &sid})
	}
	escalate := func(reason, noteJP string) (*Result, error) {
		hits := outputs.Hits
		titles := []string{}
		for _, h := range hits {
			titles = append(titles, h.Title)
		}
		evidence := strings.Join(titles, "、")
		if evidence == "" {
			evidence = "なし"
		}
		intent := "未判定"
		if outputs.Classification != nil {
			intent = string(outputs.Classification.Intent)
		}
		body := []rune(inquiry.RedactedBody)
		if len(body) > 120 {
			body = body[:120]
		}
		summary := fmt.Sprintf("【内部引き継ぎサマリー】理由: %s\n%s\n対象カテゴリ: %s\n確認した根拠: %s\n問い合わせ概要(マスキング済み): %s", reason, noteJP, intent, evidence, string(body))
		_ = st.FinishRun(runID, store.FinishRunPatch{State: "escalated", EscalationReason: &reason, HandoffSummary: &summary, UsageJSON: usageJSON(), TraceID: &traceID})
		_ = st.UpdateInquiryStatus(tenantID, inquiry.ID, "escalated")
		ah := auth.ContentHash(reason)
		_ = st.InsertAudit(store.AuditEvent{TenantID: tenantID, ActorType: "agent", ActorID: strp("agent-worker"), Action: "run.escalated", EntityType: "run", EntityID: runID, AfterHash: &ah, TraceID: &traceID})
		rootSpan.SetStatus(codes.Ok, "")
		return &Result{RunID: runID, Outcome: "escalated", EscalationReason: reason, TraceID: traceID}, nil
	}
	fail := func(code string) (*Result, error) {
		_ = st.FinishRun(runID, store.FinishRunPatch{State: "failed", FailureCode: &code, UsageJSON: usageJSON(), TraceID: &traceID})
		_ = st.UpdateInquiryStatus(tenantID, inquiry.ID, "failed")
		rootSpan.SetStatus(codes.Error, code)
		return &Result{RunID: runID, Outcome: "failed", FailureCode: code, TraceID: traceID}, nil
	}

	_ = st.UpdateInquiryStatus(tenantID, inquiry.ID, "processing")

	// Step 0 — normalize/redact (idempotent defence in depth).
	if !done["normalize_redact"] {
		t0 := time.Now()
		_, span := tracer.Start(ctx, "normalize_redact")
		_ = RedactPII(inquiry.RedactedBody)
		if err := checkpoint("normalize_redact", map[string]bool{"redacted": true}, span, t0, nil); err != nil {
			return nil, err
		}
		span.End()
	}
	text := RedactPII(inquiry.RedactedBody).Text

	// Step 1 — classify.
	if !done["classify"] {
		t0 := time.Now()
		_, span := tracer.Start(ctx, "classify")
		modelCalls++
		if err := checkLimits(); err != nil {
			span.End()
			return failOrEscalate(err, escalate, fail)
		}
		payload, _ := json.Marshal(map[string]string{"text": text})
		res, err := llm.Chat(ctx, []modelrail.ChatMessage{
			{Role: "system", Content: "TASK:CLASSIFY — return JSON {intent, urgency, confidence} using the fixed Benri taxonomy."},
			{Role: "user", Content: string(payload)},
		}, limits.MaxOutputTokens)
		if err != nil {
			span.End()
			return failOrEscalate(err, escalate, fail)
		}
		usage.PromptTokens += res.Usage.PromptTokens
		usage.CompletionTokens += res.Usage.CompletionTokens
		usage.TotalTokens += res.Usage.TotalTokens
		var c Classification
		if err := json.Unmarshal([]byte(res.Content), &c); err != nil || !domain.ValidIntent(string(c.Intent)) {
			span.End()
			return fail("CLASSIFICATION_SCHEMA_INVALID")
		}
		outputs.Classification = &c
		if err := checkpoint("classify", c, span, t0, nil); err != nil {
			span.End()
			return nil, err
		}
		span.End()
	}
	classification := outputs.Classification
	intent := classification.Intent

	// Step 2 — retrieve.
	if !done["retrieve"] {
		t0 := time.Now()
		_, span := tracer.Start(ctx, "retrieve")
		toolCalls++
		if err := checkLimits(); err != nil {
			span.End()
			return failOrEscalate(err, escalate, fail)
		}
		tt := time.Now()
		var hits []retrieval.SearchHit
		if hooks.DisableRetrieval {
			recordTool("kb-mcp", "search_kb", "read", map[string]string{"intent": string(intent)}, "ok", 0, span.SpanContext().SpanID().String())
			hits = []retrieval.SearchHit{}
		} else {
			h, err := tools.SearchKB(ctx, tenantID, inquiry.Subject+" "+text, string(intent), 5)
			if err != nil {
				span.End()
				return fail("RETRIEVAL_FAILED")
			}
			hits = h
			recordTool("kb-mcp", "search_kb", "read", map[string]any{"intent": string(intent), "limit": 5}, "ok", time.Since(tt).Milliseconds(), span.SpanContext().SpanID().String())
		}
		outputs.Hits = hits
		if err := checkpoint("retrieve", map[string]any{"hits": hits}, span, t0, nil); err != nil {
			span.End()
			return nil, err
		}
		span.End()
	}
	hits := outputs.Hits
	if hits == nil {
		hits = []retrieval.SearchHit{}
	}

	// Step 3 — order lookup.
	orderHint := inquiry.OrderNoHint
	if !done["lookup_order"] {
		t0 := time.Now()
		_, span := tracer.Start(ctx, "lookup_order")
		var order *OrderContext
		found := false
		if orderHint != nil && *orderHint != "" {
			toolCalls++
			if err := checkLimits(); err != nil {
				span.End()
				return failOrEscalate(err, escalate, fail)
			}
			tt := time.Now()
			row, err := tools.LookupOrder(ctx, tenantID, *orderHint)
			if err != nil {
				span.End()
				return fail("LOOKUP_FAILED")
			}
			status := "not_found"
			if row != nil {
				status = "ok"
				if hooks.MalformedToolOutput {
					row = nil
					status = "malformed"
				}
			}
			recordTool("crm-mcp", "lookup_order", "read", map[string]string{"order_no": MaskOrderNo(*orderHint)}, status, time.Since(tt).Milliseconds(), span.SpanContext().SpanID().String())
			if row != nil {
				order = &OrderContext{OrderNoMasked: MaskOrderNo(*orderHint), Item: row.Item, DeliveryStatus: row.DeliveryStatus, OrderedAt: row.OrderedAt}
				found = true
			}
		}
		outputs.Order, outputs.OrderFound = order, found
		if err := checkpoint("lookup_order", map[string]any{"order": order, "found": found}, span, t0, nil); err != nil {
			span.End()
			return nil, err
		}
		span.End()
	}
	order := outputs.Order

	// Step 4 — deterministic policy.
	var decision PolicyDecision
	{
		t0 := time.Now()
		_, span := tracer.Start(ctx, "policy_decision")
		evidenceTexts := []string{}
		for _, h := range hits {
			if art, err := st.GetArticle(tenantID, h.ArticleID); err == nil && art != nil {
				evidenceTexts = append(evidenceTexts, art.Body)
			} else {
				evidenceTexts = append(evidenceTexts, h.Excerpt)
			}
		}
		ambiguous := strings.Count(text, "[注文番号]") >= 2 || strings.Contains(text, "どちらか分かりません")
		decision = EvaluatePolicy(PolicyInput{
			Text: text, Classification: *classification, EvidenceCount: len(hits),
			OrderRequired: false, OrderFound: outputs.OrderFound, OrderHintPresent: orderHint != nil && *orderHint != "",
			OrderAmbiguous: ambiguous, EvidenceTexts: evidenceTexts,
		})
		if !done["policy_decision"] {
			if err := checkpoint("policy_decision", decision, span, t0, nil); err != nil {
				span.End()
				return nil, err
			}
		}
		span.End()
	}
	if decision.Escalate {
		return escalate(decision.Reason, decision.NoteJP)
	}

	// Evidence bundle.
	evidence := []EvidenceItem{}
	for _, h := range hits {
		body := h.Excerpt
		if art, err := st.GetArticle(tenantID, h.ArticleID); err == nil && art != nil {
			body = art.Body
		}
		evidence = append(evidence, EvidenceItem{ArticleID: h.ArticleID, ChunkID: h.ChunkID, Title: h.Title, Text: body})
	}
	articleTenant := map[string]string{}
	if arts, err := st.ListArticles(tenantID); err == nil {
		for _, a := range arts {
			articleTenant[a.ID] = tenantID
		}
	}
	orderText := ""
	if order != nil {
		orderText = order.Item + " " + order.DeliveryStatus
	}

	// Step 5 — draft.
	var draft DraftOutput
	{
		t0 := time.Now()
		_, span := tracer.Start(ctx, "draft")
		modelCalls++
		if err := checkLimits(); err != nil {
			span.End()
			return failOrEscalate(err, escalate, fail)
		}
		payload, _ := json.Marshal(map[string]any{"intent": string(intent), "evidence": evidence, "order": order, "query": inquiry.Subject + " " + text})
		res, err := llm.Chat(ctx, []modelrail.ChatMessage{
			{Role: "system", Content: "TASK:DRAFT — return JSON DraftOutput {body, citedArticleIds, factualReferences} grounded only in the evidence bundle."},
			{Role: "user", Content: string(payload)},
		}, limits.MaxOutputTokens)
		if err != nil {
			span.End()
			return failOrEscalate(err, escalate, fail)
		}
		usage.PromptTokens += res.Usage.PromptTokens
		usage.CompletionTokens += res.Usage.CompletionTokens
		usage.TotalTokens += res.Usage.TotalTokens
		if err := json.Unmarshal([]byte(res.Content), &draft); err != nil {
			span.End()
			return escalate(domain.EscVerifyFailed, "ドラフトの検証に失敗したため、担当者へ引き継ぎます。")
		}
		if !done["draft"] {
			if err := checkpoint("draft", map[string]any{"cited": draft.CitedArticleIDs}, span, t0, nil); err != nil {
				span.End()
				return nil, err
			}
		}
		span.End()
	}

	// Step 6 — verify (one bounded repair attempt, then escalate).
	toVerify := func(d DraftOutput) verify.Result {
		vEv := make([]verify.EvidenceItem, 0, len(evidence))
		for _, e := range evidence {
			vEv = append(vEv, verify.EvidenceItem{ArticleID: e.ArticleID, Text: e.Text})
		}
		var vOrder *verify.OrderContext
		if order != nil {
			vOrder = &verify.OrderContext{DeliveryStatus: order.DeliveryStatus, Item: order.Item}
		}
		refs := make([]verify.FactualReference, 0, len(d.FactualReferences))
		for _, r := range d.FactualReferences {
			refs = append(refs, verify.FactualReference{FactKey: r.FactKey, NormalizedValue: r.NormalizedValue, SourceArticleID: r.SourceArticleID, SourceChunkID: r.SourceChunkID})
		}
		return verify.VerifyDraft(verify.DraftLike{Body: d.Body, CitedArticleIDs: d.CitedArticleIDs, FactualReferences: refs},
			verify.Context{TenantID: tenantID, Evidence: vEv, ArticleTenant: articleTenant, Order: vOrder, OrderText: orderText})
	}
	verification := toVerify(draft)
	if !verification.Passed {
		draft = BuildDraft(string(intent), evidence, order, inquiry.Subject+" "+text, defaultTokenizer())
		verification = toVerify(draft)
	}
	{
		t0 := time.Now()
		_, span := tracer.Start(ctx, "verify")
		if !done["verify"] {
			if err := checkpoint("verify", map[string]bool{"passed": verification.Passed}, span, t0, nil); err != nil {
				span.End()
				return nil, err
			}
		}
		span.End()
	}
	if !verification.Passed {
		return escalate(domain.EscVerifyFailed, "ドラフトの検証に失敗したため、担当者へ引き継ぎます。")
	}

	// Step 7 — persist draft + deterministic proposed actions; stop at review.
	revision, err := st.NextRevision(runID)
	if err != nil {
		return nil, err
	}
	draftID := store.NewID("draft")
	contentHash := auth.ContentHash(draft.Body)
	citedJSON, _ := json.Marshal(draft.CitedArticleIDs)
	refsJSON, _ := json.Marshal(draft.FactualReferences)
	verJSON, _ := json.Marshal(verification)
	verStr := string(verJSON)
	if _, err := st.InsertDraft(store.Draft{ID: draftID, TenantID: tenantID, RunID: runID, Revision: revision, Body: draft.Body, CitedArticleIDs: string(citedJSON), FactualReferences: string(refsJSON), VerificationJSON: &verStr, ContentHash: contentHash}); err != nil {
		return nil, err
	}
	for _, actionType := range ActionTypesForIntent(intent) {
		payload := BuildCanonicalPayload(tenantID, inquiry.ID, draftID, revision, actionType, draft.Body, inquiry.Subject, classification.Urgency)
		payloadJSON, _ := json.Marshal(payload)
		if _, err := st.InsertProposedAction(store.ProposedAction{ID: store.NewID("action"), TenantID: tenantID, DraftID: draftID, RunID: &runID, ActionType: actionType, CanonicalPayload: string(payloadJSON), PayloadHash: PayloadHash(payload)}); err != nil {
			return nil, err
		}
	}
	_ = st.FinishRun(runID, store.FinishRunPatch{State: "completed", UsageJSON: usageJSON(), TraceID: &traceID})
	_ = st.UpdateInquiryStatus(tenantID, inquiry.ID, "needs_review")
	_ = st.InsertAudit(store.AuditEvent{TenantID: tenantID, ActorType: "agent", ActorID: strp("agent-worker"), Action: "run.completed_awaiting_review", EntityType: "run", EntityID: runID, AfterHash: &contentHash, TraceID: &traceID})
	rootSpan.SetStatus(codes.Ok, "")
	return &Result{RunID: runID, Outcome: "awaiting_review", DraftID: draftID, TraceID: traceID}, nil
}

func strp(s string) *string { return &s }

func failOrEscalate(err error, escalate func(string, string) (*Result, error), fail func(string) (*Result, error)) (*Result, error) {
	if ge, ok := err.(*modelrail.GatewayError); ok {
		_ = ge
		return fail("GATEWAY_UNAVAILABLE")
	}
	if de, ok := err.(*domain.Error); ok {
		if de.Code == domain.EscBudgetExceeded {
			return escalate(domain.EscBudgetExceeded, "モデル呼び出し上限に達したため、担当者へ引き継ぎます。")
		}
		return fail(de.Code)
	}
	return fail("INTERNAL_ERROR")
}

// defaultTokenizer is set by the worker/eval entry points via SetTokenizer
// so the repair path uses the same Kagome dictionary as retrieval.
var tokenizerMu = struct {
	tk interface {
		Name() string
		Tokenize(string) []string
	}
}{}

func SetTokenizer(tk interface {
	Name() string
	Tokenize(string) []string
}) {
	tokenizerMu.tk = tk
}
func defaultTokenizer() interface {
	Name() string
	Tokenize(string) []string
} {
	return tokenizerMu.tk
}
