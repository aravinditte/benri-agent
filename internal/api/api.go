// Package api implements the HTTP API (SPEC §12) on the oapi-codegen chi
// server stubs generated from api/openapi.yaml. The handler holds an
// "api"-role store handle: it can approve drafts and insert execution jobs,
// but structurally cannot write tickets or outbox records (SPEC §9).
package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/api/gen"
	"github.com/aravinditte/benri-agent/internal/approval"
	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/verify"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	Store store.Repository // api role
	Cfg   *config.Config
	TK    japanese.Tokenizer
	gen.Unimplemented
}

// Router mounts the generated routes on chi.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	return gen.HandlerWithOptions(s, gen.ChiServerOptions{BaseRouter: r})
}

// ---- helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("content-type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, message string) {
	var e gen.Error
	e.Error.Code = code
	e.Error.Message = message
	writeJSON(w, status, e)
}

func statusForCode(code string) int {
	switch {
	case code == "UNAUTHORIZED":
		return 401
	case code == "FORBIDDEN" || code == "CAPABILITY_DENIED":
		return 403
	case strings.HasSuffix(code, "_NOT_FOUND") || code == "NOT_FOUND" || code == "DRAFT_NOT_FOUND" || code == "ACTION_NOT_FOUND" || code == "INQUIRY_NOT_FOUND":
		return 404
	case strings.Contains(code, "MISMATCH") || strings.Contains(code, "NOT_PENDING") || strings.Contains(code, "NOT_PROPOSED") || code == "REVISION_MISMATCH":
		return 409
	case code == "INVALID_CREDENTIALS":
		return 401
	default:
		return 400
	}
}

func writeDomainErr(w http.ResponseWriter, err error) {
	if de, ok := err.(*domain.Error); ok {
		writeErr(w, statusForCode(de.Code), de.Code, de.Msg)
		return
	}
	if ce, ok := err.(*store.CapabilityError); ok {
		writeErr(w, 403, ce.Code(), ce.Msg)
		return
	}
	writeErr(w, 500, "INTERNAL", err.Error())
}

func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) *store.User {
	h := r.Header.Get("authorization")
	if !strings.HasPrefix(h, "Bearer ") {
		writeErr(w, 401, "UNAUTHORIZED", "authentication required")
		return nil
	}
	user, err := s.Store.FindUserBySession(auth.HashToken(strings.TrimPrefix(h, "Bearer ")))
	if err != nil || user == nil {
		writeErr(w, 401, "UNAUTHORIZED", "authentication required")
		return nil
	}
	return user
}

func requireAdmin(w http.ResponseWriter, user *store.User) bool {
	if user.Role != "admin" {
		writeErr(w, 403, "FORBIDDEN", "admin role required")
		return false
	}
	return true
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeErr(w, 400, "VALIDATION_ERROR", "request body must be valid JSON")
		return false
	}
	return true
}

func genUser(u *store.User) gen.User {
	return gen.User{Id: u.ID, TenantId: u.TenantID, Email: u.Email, Role: gen.UserRole(u.Role)}
}

// ---- health & auth ----

func (s *Server) GetHealth(w http.ResponseWriter, r *http.Request) {
	mode := gen.HealthMode("live")
	label := s.Cfg.ModelRailModel + " via ModelRail"
	if s.Cfg.MockLLM || s.Cfg.ModelRailBaseURL == "" {
		mode = gen.HealthMode("mock")
		label = "mock LLM (deterministic, MOCK_LLM=1)"
	}
	writeJSON(w, 200, gen.Health{Status: "ok", Mode: mode, LlmLabel: label})
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var body gen.LoginJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	user, err := s.Store.FindUserByEmail(body.Email)
	if err != nil || user == nil || !auth.VerifyPassword(body.Password, user.PasswordHash) {
		writeErr(w, 401, "INVALID_CREDENTIALS", "email or password is incorrect")
		return
	}
	raw := make([]byte, 24)
	_, _ = rand.Read(raw)
	token := "benri_" + hex.EncodeToString(raw)
	if err := s.Store.CreateSession(auth.HashToken(token), user.ID); err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, 200, gen.LoginResponse{Token: token, User: genUser(user)})
}

func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	if user := s.authenticate(w, r); user == nil {
		return
	}
	token := strings.TrimPrefix(r.Header.Get("authorization"), "Bearer ")
	_ = s.Store.DeleteSession(auth.HashToken(token))
	writeJSON(w, 200, gen.Ok{Ok: true})
}

func (s *Server) GetSession(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	writeJSON(w, 200, gen.SessionResponse{User: genUser(user)})
}

// ---- inquiries ----

func genInquiry(i store.Inquiry) gen.Inquiry {
	return gen.Inquiry{Id: i.ID, TenantId: i.TenantID, Source: &i.Source, Subject: i.Subject, RedactedBody: i.RedactedBody, NormalizedBodyHash: &i.NormalizedBodyHash, OrderNoHint: i.OrderNoHint, Status: i.Status, CreatedAt: i.CreatedAt}
}

func (s *Server) ListInquiries(w http.ResponseWriter, r *http.Request, params gen.ListInquiriesParams) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	state := ""
	if params.State != nil {
		state = *params.State
	}
	rows, err := s.Store.ListInquiries(user.TenantID, state)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.InquiryList{Inquiries: []gen.Inquiry{}}
	for _, row := range rows {
		out.Inquiries = append(out.Inquiries, genInquiry(row))
	}
	writeJSON(w, 200, out)
}

func (s *Server) CreateInquiry(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	var body gen.CreateInquiryJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Subject) == "" || strings.TrimSpace(body.Body) == "" {
		writeErr(w, 400, "VALIDATION_ERROR", "subject and body are required")
		return
	}
	redacted := agent.RedactPII(body.Body).Text
	row, err := s.Store.InsertInquiry(store.Inquiry{
		ID: store.NewID("inq"), TenantID: user.TenantID, Source: "api", Subject: body.Subject,
		RedactedBody: redacted, NormalizedBodyHash: auth.ContentHash(japanese.Normalize(redacted)),
		OrderNoHint: body.OrderNoHint, Status: "new",
	})
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, 201, gen.InquiryResponse{Inquiry: genInquiry(*row)})
}

func genRunSummary(rn store.Run) gen.RunSummary {
	return gen.RunSummary{Id: rn.ID, TenantId: rn.TenantID, InquiryId: rn.InquiryID, State: rn.State, ModelRoute: rn.ModelRoute, PromptVersion: rn.PromptVersion, TraceId: rn.TraceID, EscalationReason: rn.EscalationReason, FailureCode: rn.LastErrorCode, CreatedAt: rn.CreatedAt}
}

func (s *Server) GetInquiry(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	row, err := s.Store.GetInquiry(user.TenantID, id)
	if err != nil || row == nil {
		writeErr(w, 404, "INQUIRY_NOT_FOUND", "inquiry not found")
		return
	}
	runs, _ := s.Store.ListRunsForInquiry(user.TenantID, id)
	detail := gen.InquiryDetail{Inquiry: genInquiry(*row), Runs: []gen.RunSummary{}}
	for _, rn := range runs {
		detail.Runs = append(detail.Runs, genRunSummary(rn))
	}
	writeJSON(w, 200, detail)
}

func (s *Server) CreateRun(w http.ResponseWriter, r *http.Request, id gen.Id, params gen.CreateRunParams) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	var body gen.CreateRunJSONRequestBody
	_ = json.NewDecoder(r.Body).Decode(&body)
	inquiry, err := s.Store.GetInquiry(user.TenantID, id)
	if err != nil || inquiry == nil {
		writeErr(w, 404, "INQUIRY_NOT_FOUND", "inquiry not found")
		return
	}
	requestKey := ""
	if params.IdempotencyKey != nil {
		requestKey = *params.IdempotencyKey
	}
	if requestKey == "" && body.RequestKey != nil {
		requestKey = *body.RequestKey
	}
	if requestKey == "" {
		requestKey = "manual"
	}
	idem := "run:" + user.TenantID + ":" + id + ":" + requestKey
	pv := domain.PromptVersion
	route := "mock-deterministic-v1"
	if !s.Cfg.MockLLM && s.Cfg.ModelRailBaseURL != "" {
		route = s.Cfg.ModelRailModel
	}
	traceID := newTraceID()
	run, err := s.Store.CreateRun(store.Run{
		ID: store.NewID("run"), TenantID: user.TenantID, InquiryID: id,
		PromptVersion: &pv, ModelRoute: &route, TraceID: &traceID, IdempotencyKey: &idem,
	})
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, 201, gen.CreateRunResponse{RunId: run.ID, State: run.State})
}

func newTraceID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ---- runs & drafts ----

func genDraft(d store.Draft) gen.Draft {
	out := gen.Draft{Id: d.ID, TenantId: d.TenantID, RunId: d.RunID, Revision: d.Revision, Body: d.Body, ContentHash: d.ContentHash, State: d.State, CreatedAt: d.CreatedAt, CitedArticleIds: []string{}}
	_ = json.Unmarshal([]byte(d.CitedArticleIDs), &out.CitedArticleIds)
	if out.CitedArticleIds == nil {
		out.CitedArticleIds = []string{}
	}
	var refs []gen.FactualReference
	if err := json.Unmarshal([]byte(d.FactualReferences), &refs); err == nil && len(refs) > 0 {
		out.FactualReferences = &refs
	}
	if d.VerificationJSON != nil {
		var v gen.Verification
		if err := json.Unmarshal([]byte(*d.VerificationJSON), &v); err == nil {
			out.Verification = &v
		}
	}
	return out
}

func genAction(a store.ProposedAction) gen.ProposedAction {
	out := gen.ProposedAction{Id: a.ID, TenantId: a.TenantID, DraftId: a.DraftID, RunId: a.RunID, ActionType: a.ActionType, PayloadHash: a.PayloadHash, State: a.State, CreatedAt: &a.CreatedAt}
	var payload map[string]any
	if err := json.Unmarshal([]byte(a.CanonicalPayload), &payload); err == nil {
		out.CanonicalPayload = &payload
	}
	return out
}

func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	run, err := s.Store.GetRun(user.TenantID, id)
	if err != nil || run == nil {
		writeErr(w, 404, "RUN_NOT_FOUND", "run not found")
		return
	}
	steps, _ := s.Store.GetCheckpoints(user.TenantID, id)
	calls, _ := s.Store.ListToolCalls(user.TenantID, id)
	detail := gen.RunDetail{Run: genRunSummary(*run), Steps: []gen.RunStep{}, ToolCalls: []gen.ToolCall{}, Evidence: []gen.Evidence{}, ProposedActions: []gen.ProposedAction{}}
	for _, st := range steps {
		var dur *int
		if st.DurationMS != nil {
			d := int(*st.DurationMS)
			dur = &d
		}
		detail.Steps = append(detail.Steps, gen.RunStep{Id: st.ID, RunId: st.RunID, StepKey: st.StepKey, Attempt: st.Attempt, State: st.State, OutputRedacted: st.OutputRedacted, DurationMs: dur, TraceId: st.TraceID, SpanId: st.SpanID, ErrorCode: st.ErrorCode})
	}
	for _, c := range calls {
		var lat *int
		if c.LatencyMS != nil {
			l := int(*c.LatencyMS)
			lat = &l
		}
		detail.ToolCalls = append(detail.ToolCalls, gen.ToolCall{Id: c.ID, Server: c.Server, Tool: c.Tool, RiskLevel: c.RiskLevel, ArgsRedacted: c.ArgsRedacted, Status: c.Status, LatencyMs: lat})
	}
	for _, st := range steps {
		if st.StepKey == "retrieve" && st.OutputRedacted != nil {
			var wrap struct {
				Hits []struct {
					ArticleID string `json:"article_id"`
					Title     string `json:"title"`
					Excerpt   string `json:"excerpt"`
				} `json:"hits"`
			}
			if err := json.Unmarshal([]byte(*st.OutputRedacted), &wrap); err == nil {
				for _, h := range wrap.Hits {
					detail.Evidence = append(detail.Evidence, gen.Evidence{ArticleId: h.ArticleID, Title: h.Title, Excerpt: h.Excerpt})
				}
			}
		}
	}
	if draft, _ := s.Store.GetDraftByRun(user.TenantID, id); draft != nil {
		gd := genDraft(*draft)
		detail.Draft = &gd
		actions, _ := s.Store.ListActionsByDraft(user.TenantID, draft.ID)
		for _, a := range actions {
			detail.ProposedActions = append(detail.ProposedActions, genAction(a))
		}
	}
	if run.EscalationReason != nil {
		summary := ""
		if run.HandoffSummary != nil {
			summary = *run.HandoffSummary
		}
		detail.Escalation = &gen.Escalation{ReasonCode: *run.EscalationReason, Summary: summary}
	}
	writeJSON(w, 200, detail)
}

func (s *Server) GetDraft(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	draft, err := s.Store.GetDraft(user.TenantID, id)
	if err != nil || draft == nil {
		writeErr(w, 404, "DRAFT_NOT_FOUND", "draft not found")
		return
	}
	actions, _ := s.Store.ListActionsByDraft(user.TenantID, id)
	detail := gen.DraftDetail{Draft: genDraft(*draft), ProposedActions: []gen.ProposedAction{}}
	for _, a := range actions {
		detail.ProposedActions = append(detail.ProposedActions, genAction(a))
	}
	writeJSON(w, 200, detail)
}

func (s *Server) ApproveDraft(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	var body gen.ApproveDraftJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	draft, err := s.Store.GetDraft(user.TenantID, id)
	if err != nil || draft == nil {
		writeErr(w, 404, "DRAFT_NOT_FOUND", "draft not found")
		return
	}
	// The canonical contract carries revision + selected action ids; the
	// payload hashes bound into the approval record are the stored hashes
	// shown to the operator in the draft detail (see VERIFICATION-REPORT).
	actions, _ := s.Store.ListActionsByDraft(user.TenantID, id)
	byID := map[string]store.ProposedAction{}
	for _, a := range actions {
		byID[a.ID] = a
	}
	selected := []store.SelectedAction{}
	for _, aid := range body.SelectedActionIds {
		a, ok := byID[aid]
		if !ok {
			writeErr(w, 404, "ACTION_NOT_FOUND", "action "+aid+" not on this draft")
			return
		}
		selected = append(selected, store.SelectedAction{ActionID: aid, PayloadHash: a.PayloadHash})
	}
	reverify := func(d store.Draft, runID string) (bool, string) {
		return approval.ReverifyDraft(s.Store, d, runID)
	}
	if _, err := approval.Approve(s.Store, user.TenantID, id, body.Revision, draft.ContentHash, selected, user.ID, reverify); err != nil {
		writeDomainErr(w, err)
		return
	}
	resp := gen.ApproveResponse{ExecutionJobIds: []string{}}
	if len(selected) > 0 {
		if ap, _ := s.Store.GetApprovalForAction(selected[0].ActionID); ap != nil {
			resp.ApprovalId = ap.ID
		}
	}
	for _, sel := range selected {
		if job, _ := s.Store.GetJobByAction(sel.ActionID); job != nil {
			resp.ExecutionJobIds = append(resp.ExecutionJobIds, job.ID)
		}
	}
	sort.Strings(resp.ExecutionJobIds)
	writeJSON(w, 200, resp)
}

func (s *Server) RejectDraft(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	var body gen.RejectDraftJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	if _, err := approval.Reject(s.Store, user.TenantID, id, user.ID, body.Reason); err != nil {
		writeDomainErr(w, err)
		return
	}
	writeJSON(w, 200, gen.Ok{Ok: true})
}

// CreateDraftRevision creates a new immutable revision of a pending draft:
// prior version-verified factual references whose VALUES still appear in
// the edited body are kept; the edited draft is re-verified against the
// run's retrieved evidence + CRM order context before it can be approved.
func (s *Server) CreateDraftRevision(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	var body gen.CreateDraftRevisionJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	current, err := s.Store.GetDraft(user.TenantID, id)
	if err != nil || current == nil {
		writeErr(w, 404, "DRAFT_NOT_FOUND", "draft not found")
		return
	}
	if current.State != "pending_review" {
		writeErr(w, 409, "DRAFT_NOT_PENDING", "draft is "+current.State)
		return
	}
	if strings.TrimSpace(body.Body) == "" {
		writeErr(w, 400, "VALIDATION_ERROR", "body is required")
		return
	}
	// Rebuild verification context from checkpoints, as at approval time.
	evidence := []verify.EvidenceItem{}
	var order *verify.OrderContext
	orderText := ""
	evidenceBundle := []agent.EvidenceItem{}
	cps, _ := s.Store.GetCheckpoints(user.TenantID, current.RunID)
	for _, cp := range cps {
		if cp.OutputRedacted == nil {
			continue
		}
		switch cp.StepKey {
		case "retrieve":
			var wrap struct {
				Hits []struct {
					ArticleID string `json:"article_id"`
					ChunkID   string `json:"chunk_id"`
					Title     string `json:"title"`
				} `json:"hits"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			for _, h := range wrap.Hits {
				if art, err := s.Store.GetArticle(user.TenantID, h.ArticleID); err == nil && art != nil {
					evidence = append(evidence, verify.EvidenceItem{ArticleID: art.ID, Text: art.Body})
					evidenceBundle = append(evidenceBundle, agent.EvidenceItem{ArticleID: art.ID, ChunkID: h.ChunkID, Title: h.Title, Text: art.Body})
				}
			}
		case "lookup_order":
			var wrap struct {
				Order *agent.OrderContext `json:"order"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			if wrap.Order != nil {
				order = &verify.OrderContext{DeliveryStatus: wrap.Order.DeliveryStatus, Item: wrap.Order.Item}
				orderText = wrap.Order.Item + " " + wrap.Order.DeliveryStatus
			}
		}
	}
	var oldRefs []agent.FactualReference
	_ = json.Unmarshal([]byte(current.FactualReferences), &oldRefs)
	kept := []agent.FactualReference{}
	normBody := japanese.Normalize(body.Body)
	for _, ref := range oldRefs {
		if strings.Contains(normBody, japanese.Normalize(ref.NormalizedValue)) {
			kept = append(kept, ref)
		}
	}
	var cited []string
	_ = json.Unmarshal([]byte(current.CitedArticleIDs), &cited)
	vRefs := make([]verify.FactualReference, 0, len(kept))
	for _, ref := range kept {
		vRefs = append(vRefs, verify.FactualReference{FactKey: ref.FactKey, NormalizedValue: ref.NormalizedValue, SourceArticleID: ref.SourceArticleID, SourceChunkID: ref.SourceChunkID})
	}
	articleTenant := map[string]string{}
	if arts, err := s.Store.ListArticles(user.TenantID); err == nil {
		for _, a := range arts {
			articleTenant[a.ID] = user.TenantID
		}
	}
	result := verify.VerifyDraft(verify.DraftLike{Body: body.Body, CitedArticleIDs: cited, FactualReferences: vRefs},
		verify.Context{TenantID: user.TenantID, Evidence: evidence, ArticleTenant: articleTenant, Order: order, OrderText: orderText})
	verJSON, _ := json.Marshal(result)
	verStr := string(verJSON)
	revision, err := s.Store.NextRevision(current.RunID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	citedJSON, _ := json.Marshal(cited)
	refsJSON, _ := json.Marshal(kept)
	newDraft, err := s.Store.InsertDraft(store.Draft{
		ID: store.NewID("draft"), TenantID: user.TenantID, RunID: current.RunID, Revision: revision,
		Body: body.Body, CitedArticleIDs: string(citedJSON), FactualReferences: string(refsJSON),
		VerificationJSON: &verStr, ContentHash: auth.ContentHash(body.Body),
	})
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	_ = s.Store.SupersedeOtherDrafts(user.TenantID, current.RunID, newDraft.ID)
	// Re-issue the deterministic template actions for the new revision.
	run, _ := s.Store.GetRunByID(current.RunID)
	_ = run
	intent := intentOfRun(s, user.TenantID, current.RunID)
	inquirySubject := ""
	urgency := "normal"
	if inq, _ := s.Store.GetInquiry(user.TenantID, runInquiryID(s, user.TenantID, current.RunID)); inq != nil {
		inquirySubject = inq.Subject
	}
	for _, actionType := range agent.ActionTypesForIntent(intent) {
		payload := agent.BuildCanonicalPayload(user.TenantID, runInquiryID(s, user.TenantID, current.RunID), newDraft.ID, revision, actionType, body.Body, inquirySubject, urgency)
		payloadJSON, _ := json.Marshal(payload)
		runID := current.RunID
		_, _ = s.Store.InsertProposedAction(store.ProposedAction{ID: store.NewID("action"), TenantID: user.TenantID, DraftID: newDraft.ID, RunID: &runID, ActionType: actionType, CanonicalPayload: string(payloadJSON), PayloadHash: agent.PayloadHash(payload)})
	}
	var gv gen.Verification
	_ = json.Unmarshal(verJSON, &gv)
	writeJSON(w, 201, gen.RevisionResponse{Draft: genDraft(*newDraft), Verification: gv})
}

func runInquiryID(s *Server, tenantID, runID string) string {
	run, err := s.Store.GetRun(tenantID, runID)
	if err != nil || run == nil {
		return ""
	}
	return run.InquiryID
}

func intentOfRun(s *Server, tenantID, runID string) domain.Intent {
	cps, _ := s.Store.GetCheckpoints(tenantID, runID)
	for _, cp := range cps {
		if cp.StepKey == "classify" && cp.OutputRedacted != nil {
			var c agent.Classification
			if err := json.Unmarshal([]byte(*cp.OutputRedacted), &c); err == nil {
				return c.Intent
			}
		}
	}
	return domain.IntentOther
}

// ---- KB (admin writes) ----

func genArticle(a store.Article) gen.Article {
	tags := a.IntentTags
	if tags == nil {
		tags = []string{}
	}
	return gen.Article{Id: a.ID, TenantId: a.TenantID, Title: a.Title, Body: a.Body, IntentTags: tags, Version: a.Version, Status: a.Status, UpdatedAt: a.UpdatedAt}
}

func (s *Server) ListKbArticles(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	rows, err := s.Store.ListArticles(user.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.ArticleList{Articles: []gen.Article{}}
	for _, row := range rows {
		out.Articles = append(out.Articles, genArticle(row))
	}
	writeJSON(w, 200, out)
}

func (s *Server) CreateKbArticle(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil || !requireAdmin(w, user) {
		return
	}
	var body gen.CreateKbArticleJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	if strings.TrimSpace(body.Title) == "" || strings.TrimSpace(body.Body) == "" {
		writeErr(w, 400, "VALIDATION_ERROR", "title and body are required")
		return
	}
	id := ""
	if body.Id != nil {
		id = *body.Id
	}
	if id == "" {
		existing, _ := s.Store.ListArticles(user.TenantID)
		max := 0
		for _, a := range existing {
			if strings.HasPrefix(a.ID, "KB-") {
				if n, err := strconv.Atoi(strings.TrimPrefix(a.ID, "KB-")); err == nil && n > max {
					max = n
				}
			}
		}
		id = "KB-" + pad3(max+1)
	}
	tags := []string{}
	if body.IntentTags != nil {
		tags = *body.IntentTags
	}
	storeArt := store.Article{ID: id, TenantID: user.TenantID, Title: body.Title, Body: body.Body, IntentTags: tags, Status: "active", Version: 1}
	if err := s.Store.InsertArticle(storeArt, retrieval.TokenizedText(body.Body, s.TK), retrieval.IndexVersion(s.TK.Name())); err != nil {
		writeDomainErr(w, err)
		return
	}
	created, _ := s.Store.GetArticle(user.TenantID, id)
	writeJSON(w, 201, gen.ArticleResponse{Article: genArticle(*created)})
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func (s *Server) UpdateKbArticle(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil || !requireAdmin(w, user) {
		return
	}
	var body gen.UpdateKbArticleJSONRequestBody
	if !decodeBody(w, r, &body) {
		return
	}
	patch := store.ArticlePatch{Title: body.Title, Body: body.Body, Status: body.Status}
	if body.IntentTags != nil {
		patch.IntentTags = *body.IntentTags
	}
	if body.Body != nil {
		tok := retrieval.TokenizedText(*body.Body, s.TK)
		ver := retrieval.IndexVersion(s.TK.Name())
		patch.TokenizedText = &tok
		patch.IndexVersion = &ver
	}
	updated, err := s.Store.UpdateArticle(user.TenantID, id, patch)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	if updated == nil {
		writeErr(w, 404, "ARTICLE_NOT_FOUND", "article not found")
		return
	}
	writeJSON(w, 200, gen.ArticleResponse{Article: genArticle(*updated)})
}

func (s *Server) ReindexKb(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil || !requireAdmin(w, user) {
		return
	}
	// Rebuild each article's chunk with the current tokenizer/index version.
	rows, err := s.Store.ListArticles(user.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	version := retrieval.IndexVersion(s.TK.Name())
	for _, a := range rows {
		_ = s.Store.InsertArticle(a, retrieval.TokenizedText(a.Body, s.TK), version)
	}
	writeJSON(w, 200, gen.ReindexResponse{Ok: true, IndexVersion: version})
}

// ---- tickets / outbox / audit ----

func (s *Server) ListTickets(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	rows, err := s.Store.ListTickets(user.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.TicketList{Tickets: []gen.Ticket{}}
	for _, t := range rows {
		out.Tickets = append(out.Tickets, gen.Ticket{Id: t.ID, TenantId: t.TenantID, InquiryId: t.InquiryID, Status: t.Status, Priority: &t.Priority, SourceActionId: t.SourceActionID, CreatedAt: &t.CreatedAt})
	}
	writeJSON(w, 200, out)
}

func (s *Server) ListOutbox(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	rows, err := s.Store.ListOutbox(user.TenantID)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.OutboxList{Outbox: []gen.OutboxMessage{}}
	for _, m := range rows {
		out.Outbox = append(out.Outbox, gen.OutboxMessage{Id: m.ID, TenantId: m.TenantID, InquiryId: m.InquiryID, DraftRevision: m.DraftRevision, Body: m.Body, DeliveryState: m.DeliveryState, DeliveryLabel: "Demo only — not sent", SourceActionId: m.SourceActionID, CreatedAt: &m.CreatedAt})
	}
	writeJSON(w, 200, out)
}

func (s *Server) ListAudit(w http.ResponseWriter, r *http.Request, params gen.ListAuditParams) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	et, ei := "", ""
	if params.EntityType != nil {
		et = *params.EntityType
	}
	if params.EntityId != nil {
		ei = *params.EntityId
	}
	rows, err := s.Store.ListAudit(user.TenantID, et, ei)
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.AuditList{AuditEvents: []gen.AuditEvent{}}
	for _, e := range rows {
		out.AuditEvents = append(out.AuditEvents, gen.AuditEvent{Id: e.ID, TenantId: e.TenantID, ActorType: e.ActorType, ActorId: e.ActorID, Action: e.Action, EntityType: e.EntityType, EntityId: e.EntityID, TraceId: e.TraceID, CreatedAt: e.CreatedAt})
	}
	writeJSON(w, 200, out)
}

// ---- eval runs ----

func genEvalSummary(r store.EvalRunRow) gen.EvalRunSummary {
	return gen.EvalRunSummary{Id: r.ID, DatasetVersion: r.DatasetVersion, PromptVersion: r.PromptVersion, Model: r.Model, IndexVersion: r.IndexVersion, CreatedAt: r.CreatedAt}
}

func (s *Server) ListEvalRuns(w http.ResponseWriter, r *http.Request) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	rows, err := s.Store.ListEvalRuns()
	if err != nil {
		writeDomainErr(w, err)
		return
	}
	out := gen.EvalRunList{EvalRuns: []gen.EvalRunSummary{}}
	for _, row := range rows {
		out.EvalRuns = append(out.EvalRuns, genEvalSummary(row))
	}
	writeJSON(w, 200, out)
}

func (s *Server) GetEvalRun(w http.ResponseWriter, r *http.Request, id gen.Id) {
	user := s.authenticate(w, r)
	if user == nil {
		return
	}
	row, err := s.Store.GetEvalRun(id)
	if err != nil || row == nil {
		writeErr(w, 404, "EVAL_RUN_NOT_FOUND", "eval run not found")
		return
	}
	detail := gen.EvalRunDetail{EvalRun: genEvalSummary(*row)}
	if row.ReportJSON != nil {
		var report map[string]any
		if err := json.Unmarshal([]byte(*row.ReportJSON), &report); err == nil {
			detail.Report = &report
		}
	}
	writeJSON(w, 200, detail)
}
