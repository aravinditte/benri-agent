package mcpclient

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/approval"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/mcpserver"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// connectInMemory wires a client session to a real MCP server over the
// SDK's in-memory transport pair (genuine JSON-RPC through the SDK).
func connectInMemory(t *testing.T, srv *mcp.Server) *session {
	t.Helper()
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	go func() { _ = srv.Run(ctx, st) }()
	client := mcp.NewClient(&mcp.Implementation{Name: "benri-test-client", Version: "3.0.0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return &session{cs: cs}
}

func TestMCPKBAndCRMOverSDK(t *testing.T) {
	path, full := testutil.SeededDB(t)
	kbH := testutil.Handle(t, path, store.RoleKBMCP)
	crmH := testutil.Handle(t, path, store.RoleCRMMCP)
	tk := testutil.Tokenizer()
	kb := connectInMemory(t, mcpserver.NewKBServer(mcpserver.KBTools{Store: kbH, TK: tk}, "tenant-hinata"))
	var hits []struct {
		ArticleID string `json:"article_id"`
	}
	toolErr, _, err := kb.callTool(context.Background(), "search_kb", map[string]any{"tenant_id": "tenant-hinata", "query": "配送状況", "intent": "delivery_status", "limit": 5}, &hits)
	if err != nil || toolErr != "" {
		t.Fatalf("search_kb: err=%v toolErr=%s", err, toolErr)
	}
	if len(hits) == 0 {
		t.Error("search_kb over MCP returned no hits")
	}
	var art struct {
		Title string `json:"title"`
	}
	toolErr, _, err = kb.callTool(context.Background(), "get_article", map[string]any{"tenant_id": "tenant-hinata", "article_id": "KB-001"}, &art)
	if err != nil || toolErr != "" || art.Title == "" {
		t.Errorf("get_article: err=%v toolErr=%s title=%q", err, toolErr, art.Title)
	}
	// Cross-tenant argument must be refused (tenant is bound at launch).
	toolErr, _, _ = kb.callTool(context.Background(), "search_kb", map[string]any{"tenant_id": "tenant-sakura", "query": "配送", "limit": 5}, &hits)
	if toolErr == "" {
		t.Error("cross-tenant search_kb must be refused")
	}
	crm := connectInMemory(t, mcpserver.NewCRMServer(mcpserver.CRMTools{Store: crmH}, "tenant-hinata"))
	var view mcpserver.OrderView
	toolErr, _, err = crm.callTool(context.Background(), "lookup_order", map[string]any{"tenant_id": "tenant-hinata", "order_no": "HIN-2026-0001"}, &view)
	if err != nil || toolErr != "" {
		t.Fatalf("lookup_order: err=%v toolErr=%s", err, toolErr)
	}
	if view.OrderNoMasked == "HIN-2026-0001" || view.Item == "" {
		t.Errorf("order view must mask the number and carry the item: %+v", view)
	}
	toolErr, _, _ = crm.callTool(context.Background(), "lookup_order", map[string]any{"tenant_id": "tenant-hinata", "order_no": "SAK-2026-0001"}, &view)
	if toolErr == "" {
		t.Error("unknown/cross-tenant order must be a tool error")
	}
	_ = full
	_ = json.Marshal
}

func TestMCPTicketServerRefusesThenSucceeds(t *testing.T) {
	path, full := testutil.SeededDB(t)
	agentH := testutil.Handle(t, path, store.RoleAgent)
	apiH := testutil.Handle(t, path, store.RoleAPI)
	ticketH := testutil.Handle(t, path, store.RoleTicketMCP)
	// Pipeline for INQ-006.
	run, err := full.CreateRun(store.Run{ID: store.NewID("run"), TenantID: "tenant-hinata", InquiryID: "INQ-006"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = full.ClaimNextRun("test", 60000)
	tk := testutil.Tokenizer()
	llm := modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	limits := domain.Limits{MaxModelCallsPerRun: 4, MaxToolCallsPerRun: 8, MaxRunDurationMS: 30000, MaxOutputTokens: 1200}
	res, err := agent.RunPipeline(context.Background(), agentH, run.ID, agent.DirectToolGateway{Store: agentH, TK: tk}, llm, limits, nil)
	if err != nil || res.Outcome != "awaiting_review" {
		t.Fatalf("pipeline: %v %+v", err, res)
	}
	draft, _ := full.GetDraftByRun("tenant-hinata", run.ID)
	actions, _ := full.ListActionsByDraft("tenant-hinata", draft.ID)
	var ticketAction store.ProposedAction
	for _, a := range actions {
		if a.ActionType == "create_ticket" {
			ticketAction = a
		}
	}
	sess := connectInMemory(t, mcpserver.NewTicketServer(mcpserver.TicketTools{Store: ticketH}))
	var out mcpserver.TicketResult
	// Unapproved: must be a tool error.
	toolErr, _, err := sess.callTool(context.Background(), "create_ticket", map[string]any{"approved_action_id": ticketAction.ID}, &out)
	if err != nil {
		t.Fatal(err)
	}
	if toolErr == "" {
		t.Error("create_ticket for an unapproved action must fail over MCP")
	}
	if n, _ := full.CountTickets(); n != 0 {
		t.Fatal("ticket created for unapproved action")
	}
	// Approve, then the same MCP call succeeds.
	sel := []store.SelectedAction{}
	for _, a := range actions {
		sel = append(sel, store.SelectedAction{ActionID: a.ID, PayloadHash: a.PayloadHash})
	}
	reverify := func(d store.Draft, runID string) (bool, string) { return approval.ReverifyDraft(apiH, d, runID) }
	if _, err := approval.Approve(apiH, "tenant-hinata", draft.ID, draft.Revision, draft.ContentHash, sel, "user-operator", reverify); err != nil {
		t.Fatal(err)
	}
	toolErr, _, err = sess.callTool(context.Background(), "create_ticket", map[string]any{"approved_action_id": ticketAction.ID}, &out)
	if err != nil || toolErr != "" {
		t.Fatalf("approved create_ticket over MCP: err=%v toolErr=%s", err, toolErr)
	}
	if out.TicketID == "" {
		t.Error("expected a ticket id")
	}
	if n, _ := full.CountTickets(); n != 1 {
		t.Errorf("tickets = %d, want 1", n)
	}
	// Replay returns the same ticket, no duplicate.
	var out2 mcpserver.TicketResult
	toolErr, _, err = sess.callTool(context.Background(), "create_ticket", map[string]any{"approved_action_id": ticketAction.ID}, &out2)
	if err != nil || toolErr != "" || !out2.Replayed || out2.TicketID != out.TicketID {
		t.Errorf("replay: err=%v toolErr=%s out=%+v", err, toolErr, out2)
	}
	if n, _ := full.CountTickets(); n != 1 {
		t.Errorf("after replay tickets = %d, want 1", n)
	}
}
