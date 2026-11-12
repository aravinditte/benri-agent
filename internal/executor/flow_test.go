package executor_test

import (
	"context"
	"testing"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/approval"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/executor"
	"github.com/aravinditte/benri-agent/internal/mcpclient"
	"github.com/aravinditte/benri-agent/internal/mcpserver"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

type flowEnv struct {
	path    string
	full    store.Repository
	agentH  store.Repository
	apiH    store.Repository
	execH   store.Repository
	ticketH store.Repository
	draft   *store.Draft
	actions []store.ProposedAction
}

func setupFlow(t *testing.T) *flowEnv {
	t.Helper()
	path, full := testutil.SeededDB(t)
	agentH := testutil.Handle(t, path, store.RoleAgent)
	apiH := testutil.Handle(t, path, store.RoleAPI)
	execH := testutil.Handle(t, path, store.RoleExecutor)
	ticketH := testutil.Handle(t, path, store.RoleTicketMCP)
	// Run the pipeline for the invoice inquiry INQ-006 (2 actions).
	run, err := full.CreateRun(store.Run{ID: store.NewID("run"), TenantID: "tenant-hinata", InquiryID: "INQ-006"})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = full.ClaimNextRun("test", 60000)
	tk := testutil.Tokenizer()
	llm := modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	limits := domain.Limits{MaxModelCallsPerRun: 4, MaxToolCallsPerRun: 8, MaxRunDurationMS: 30000, MaxOutputTokens: 1200}
	res, err := agent.RunPipeline(context.Background(), agentH, run.ID, agent.DirectToolGateway{Store: agentH, TK: tk}, llm, limits, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Outcome != "awaiting_review" {
		t.Fatalf("pipeline outcome = %s", res.Outcome)
	}
	draft, _ := full.GetDraftByRun("tenant-hinata", run.ID)
	actions, _ := full.ListActionsByDraft("tenant-hinata", draft.ID)
	if len(actions) != 2 {
		t.Fatalf("expected 2 proposed actions, got %d", len(actions))
	}
	return &flowEnv{path: path, full: full, agentH: agentH, apiH: apiH, execH: execH, ticketH: ticketH, draft: draft, actions: actions}
}

func (e *flowEnv) approve(t *testing.T, selected []store.ProposedAction) {
	t.Helper()
	sel := []store.SelectedAction{}
	for _, a := range selected {
		sel = append(sel, store.SelectedAction{ActionID: a.ID, PayloadHash: a.PayloadHash})
	}
	reverify := func(d store.Draft, runID string) (bool, string) {
		return approval.ReverifyDraft(e.apiH, d, runID)
	}
	if _, err := approval.Approve(e.apiH, "tenant-hinata", e.draft.ID, e.draft.Revision, e.draft.ContentHash, sel, "user-operator", reverify); err != nil {
		t.Fatalf("approve: %v", err)
	}
}

func (e *flowEnv) executeAll(t *testing.T) []*executor.Outcome {
	t.Helper()
	gw := mcpclient.DirectTicketGateway{Tools: mcpserver.TicketTools{Store: e.ticketH}}
	outcomes := []*executor.Outcome{}
	for {
		job, err := e.execH.ClaimNextJob("test-exec", 60000)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil || job.State != "running" {
			break
		}
		out, err := executor.ExecuteClaimedJob(context.Background(), e.execH, job, gw)
		if err != nil {
			t.Fatal(err)
		}
		outcomes = append(outcomes, out)
	}
	return outcomes
}

func TestApproveExecuteExactlyOnce(t *testing.T) {
	e := setupFlow(t)
	e.approve(t, e.actions)
	outcomes := e.executeAll(t)
	if len(outcomes) != 2 {
		t.Fatalf("expected 2 job outcomes, got %d", len(outcomes))
	}
	for _, o := range outcomes {
		if o.Status != "succeeded" {
			t.Errorf("job %s status = %s (%s)", o.JobID, o.Status, o.Detail)
		}
	}
	if n, _ := e.full.CountTickets(); n != 1 {
		t.Errorf("tickets = %d, want exactly 1", n)
	}
	if n, _ := e.full.CountOutbox(); n != 1 {
		t.Errorf("outbox = %d, want exactly 1", n)
	}
	// Replay: executing again must not duplicate.
	outcomes2 := e.executeAll(t)
	if len(outcomes2) != 0 {
		t.Errorf("no jobs should remain, got %d", len(outcomes2))
	}
	if n, _ := e.full.CountTickets(); n != 1 {
		t.Errorf("after replay tickets = %d, want 1", n)
	}
	if n, _ := e.full.CountOutbox(); n != 1 {
		t.Errorf("after replay outbox = %d, want 1", n)
	}
}

func TestReapprovalAndWrongRevisionRejected(t *testing.T) {
	e := setupFlow(t)
	sel := []store.SelectedAction{{ActionID: e.actions[0].ID, PayloadHash: e.actions[0].PayloadHash}}
	reverify := func(d store.Draft, runID string) (bool, string) { return approval.ReverifyDraft(e.apiH, d, runID) }
	// Wrong revision first.
	_, err := approval.Approve(e.apiH, "tenant-hinata", e.draft.ID, e.draft.Revision+9, e.draft.ContentHash, sel, "user-operator", reverify)
	if err == nil || domain.CodeOf(err) != "REVISION_MISMATCH" {
		t.Errorf("wrong revision: err = %v", err)
	}
	// Wrong payload hash.
	bad := []store.SelectedAction{{ActionID: e.actions[0].ID, PayloadHash: "deadbeef"}}
	_, err = approval.Approve(e.apiH, "tenant-hinata", e.draft.ID, e.draft.Revision, e.draft.ContentHash, bad, "user-operator", reverify)
	if err == nil || domain.CodeOf(err) != "ACTION_HASH_MISMATCH" {
		t.Errorf("wrong hash: err = %v", err)
	}
	e.approve(t, e.actions)
	// Re-approval of a decided draft.
	_, err = approval.Approve(e.apiH, "tenant-hinata", e.draft.ID, e.draft.Revision, e.draft.ContentHash, sel, "user-operator", reverify)
	if err == nil || domain.CodeOf(err) != "DRAFT_NOT_PENDING" {
		t.Errorf("re-approval: err = %v", err)
	}
}

func TestTicketToolRefusesUnapproved(t *testing.T) {
	e := setupFlow(t)
	tools := mcpserver.TicketTools{Store: e.ticketH}
	var ticketAction *store.ProposedAction
	for i := range e.actions {
		if e.actions[i].ActionType == "create_ticket" {
			ticketAction = &e.actions[i]
		}
	}
	if ticketAction == nil {
		t.Fatal("no create_ticket action")
	}
	if _, err := tools.CreateTicket(context.Background(), mcpserver.ActionInput{ApprovedActionID: ticketAction.ID}); err == nil {
		t.Error("ticket tool must refuse an unapproved action")
	}
	if n, _ := e.full.CountTickets(); n != 0 {
		t.Error("no ticket may be created for an unapproved action")
	}
}

func TestPartialApproval(t *testing.T) {
	e := setupFlow(t)
	var replyOnly []store.ProposedAction
	for _, a := range e.actions {
		if a.ActionType == "send_demo_reply" {
			replyOnly = append(replyOnly, a)
		}
	}
	e.approve(t, replyOnly)
	outcomes := e.executeAll(t)
	if len(outcomes) != 1 {
		t.Fatalf("expected 1 job for partial approval, got %d", len(outcomes))
	}
	if n, _ := e.full.CountOutbox(); n != 1 {
		t.Errorf("outbox = %d, want 1", n)
	}
	if n, _ := e.full.CountTickets(); n != 0 {
		t.Errorf("tickets = %d, want 0 (ticket action was not approved)", n)
	}
}

type ambiguousGateway struct{}

func (ambiguousGateway) CreateTicket(ctx context.Context, id string) (string, bool, error) {
	return "", false, &executor.OutcomeUnknownError{Msg: "simulated transport loss after send"}
}
func (ambiguousGateway) AddTicketNote(ctx context.Context, id string) (string, error) {
	return "", &executor.OutcomeUnknownError{Msg: "simulated"}
}

func TestAmbiguousOutcomeBecomesUnknown(t *testing.T) {
	e := setupFlow(t)
	e.approve(t, e.actions)
	// Execute the create_ticket job with an ambiguous gateway.
	var ticketJob *store.ExecutionJob
	for {
		job, err := e.execH.ClaimNextJob("test-exec", 60000)
		if err != nil {
			t.Fatal(err)
		}
		if job == nil {
			break
		}
		action, _ := e.full.GetActionByID(job.ProposedActionID)
		if action != nil && action.ActionType == "create_ticket" {
			ticketJob = job
			break
		}
		// finish the other job normally to avoid interference
		gw := mcpclient.DirectTicketGateway{Tools: mcpserver.TicketTools{Store: e.ticketH}}
		_, _ = executor.ExecuteClaimedJob(context.Background(), e.execH, job, gw)
	}
	if ticketJob == nil {
		t.Fatal("ticket job not found")
	}
	out, err := executor.ExecuteClaimedJob(context.Background(), e.execH, ticketJob, ambiguousGateway{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Status != "unknown" {
		t.Errorf("ambiguous outcome: status = %s, want unknown", out.Status)
	}
	job, _ := e.full.GetJob(ticketJob.ID)
	if job == nil || job.State != "unknown" {
		t.Errorf("job state = %v, want unknown", job)
	}
}
