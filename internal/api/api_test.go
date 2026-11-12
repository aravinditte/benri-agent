package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	apihttp "github.com/aravinditte/benri-agent/internal/api"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

func setupAPI(t *testing.T) (http.Handler, string) {
	t.Helper()
	path, _ := testutil.SeededDB(t)
	apiH := testutil.Handle(t, path, store.RoleAPI)
	cfg := &config.Config{MockLLM: true, ModelRailModel: "default", StoreKind: "sqlite", DBPath: path}
	srv := &apihttp.Server{Store: apiH, Cfg: cfg, TK: testutil.Tokenizer()}
	return srv.Router(), path
}

func doReq(t *testing.T, h http.Handler, method, path string, body any, token string) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if body != nil {
		req.Header.Set("content-type", "application/json")
	}
	if token != "" {
		req.Header.Set("authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out == nil {
		out = map[string]any{}
	}
	return rec.Code, out
}

func login(t *testing.T, h http.Handler, email, pass string) string {
	t.Helper()
	code, body := doReq(t, h, "POST", "/auth/login", map[string]string{"email": email, "password": pass}, "")
	if code != 200 {
		t.Fatalf("login %s: %d %v", email, code, body)
	}
	return body["token"].(string)
}

func TestHealthPublicAndAuthRequired(t *testing.T) {
	h, _ := setupAPI(t)
	code, body := doReq(t, h, "GET", "/health", nil, "")
	if code != 200 || body["status"] != "ok" || body["mode"] != "mock" {
		t.Errorf("health: %d %v", code, body)
	}
	code, _ = doReq(t, h, "GET", "/inquiries", nil, "")
	if code != 401 {
		t.Errorf("unauthenticated /inquiries: %d, want 401", code)
	}
	code, body = doReq(t, h, "POST", "/auth/login", map[string]string{"email": "operator@hinata.example", "password": "wrong"}, "")
	if code != 401 {
		t.Errorf("bad login: %d, want 401", code)
	}
	errObj, _ := body["error"].(map[string]any)
	if errObj == nil || errObj["code"] != "INVALID_CREDENTIALS" {
		t.Errorf("bad login must return a stable error code: %v", body)
	}
}

func TestRunIdempotencyAndApprovalOverHTTP(t *testing.T) {
	h, path := setupAPI(t)
	token := login(t, h, "operator@hinata.example", "demo-operator-pass")
	code, r1 := doReq(t, h, "POST", "/inquiries/INQ-006/runs", map[string]any{"request_key": "smoke-1"}, token)
	if code != 201 {
		t.Fatalf("create run: %d %v", code, r1)
	}
	code, r2 := doReq(t, h, "POST", "/inquiries/INQ-006/runs", map[string]any{"request_key": "smoke-1"}, token)
	if code != 201 || r2["run_id"] != r1["run_id"] {
		t.Errorf("idempotent re-request must return the same run: %v vs %v", r1, r2)
	}
	// Drive the pipeline directly (worker equivalent), then approve over HTTP.
	full := testutil.Handle(t, path, store.RoleFull)
	_ = full
	agentH := testutil.Handle(t, path, store.RoleAgent)
	runPipelineForTest(t, agentH, r1["run_id"].(string))
	code, detail := doReq(t, h, "GET", "/runs/"+r1["run_id"].(string), nil, token)
	if code != 200 {
		t.Fatalf("run detail: %d", code)
	}
	draft, _ := detail["draft"].(map[string]any)
	actions, _ := detail["proposed_actions"].([]any)
	if draft == nil || len(actions) == 0 {
		t.Fatalf("expected draft + actions: %v", detail)
	}
	ids := []string{}
	for _, a := range actions {
		ids = append(ids, a.(map[string]any)["id"].(string))
	}
	code, resp := doReq(t, h, "POST", "/drafts/"+draft["id"].(string)+"/approve", map[string]any{"revision": draft["revision"], "selected_action_ids": ids}, token)
	if code != 200 {
		t.Fatalf("approve over HTTP: %d %v", code, resp)
	}
	jobs, _ := resp["execution_job_ids"].([]any)
	if len(jobs) != len(ids) {
		t.Errorf("expected %d jobs, got %v", len(ids), resp)
	}
}

func TestAdminOnlyKBWrites(t *testing.T) {
	h, _ := setupAPI(t)
	operator := login(t, h, "operator@hinata.example", "demo-operator-pass")
	code, _ := doReq(t, h, "POST", "/kb/articles", map[string]any{"title": "x", "body": "y"}, operator)
	if code != 403 {
		t.Errorf("operator KB write: %d, want 403", code)
	}
	admin := login(t, h, "admin@hinata.example", "demo-admin-pass")
	code, resp := doReq(t, h, "POST", "/kb/articles", map[string]any{"title": "新しい案内", "body": "テスト用の記事本文です。"}, admin)
	if code != 201 {
		t.Fatalf("admin KB write: %d %v", code, resp)
	}
	art, _ := resp["article"].(map[string]any)
	if art == nil || art["id"] == "" {
		t.Errorf("created article must carry an assigned id: %v", resp)
	}
}

func TestCreateInquiryRedactsBeforePersistence(t *testing.T) {
	h, path := setupAPI(t)
	token := login(t, h, "operator@hinata.example", "demo-operator-pass")
	code, resp := doReq(t, h, "POST", "/inquiries", map[string]any{"subject": "連絡先について", "body": "電話は090-1234-5678です。配送状況を教えてください。"}, token)
	if code != 201 {
		t.Fatalf("create inquiry: %d %v", code, resp)
	}
	inq, _ := resp["inquiry"].(map[string]any)
	if body, _ := inq["redacted_body"].(string); strings.Contains(body, "090-1234-5678") {
		t.Errorf("PII persisted unredacted: %s", body)
	}
	full := testutil.Handle(t, path, store.RoleFull)
	stored, _ := full.GetInquiry("tenant-hinata", inq["id"].(string))
	if stored == nil || strings.Contains(stored.RedactedBody, "090-1234-5678") {
		t.Error("stored inquiry body contains raw PII")
	}
}
