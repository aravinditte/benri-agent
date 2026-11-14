package store

import (
	"fmt"
	"os"
	"testing"
)

// TestPostgresRepositoryContract exercises the real PostgreSQL driver and the
// representative read/write path used by Compose. CI supplies a migrated,
// disposable database; local runs skip when no URL is configured.
func TestPostgresRepositoryContract(t *testing.T) {
	url := os.Getenv("BENRI_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("BENRI_TEST_POSTGRES_URL is not set")
	}
	st, err := OpenPG(url, RoleFull)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	suffix := fmt.Sprintf("%x", NewID("pg"))
	tenantID := "tenant-" + suffix
	userID := "user-" + suffix
	inquiryID := "inq-" + suffix
	customerID := "customer-" + suffix
	orderNo := "ORDER-" + suffix
	articleID := "article-" + suffix

	if err := st.InsertTenant(tenantID, "Postgres Contract Tenant"); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertUser(User{ID: userID, TenantID: tenantID, Email: suffix + "@example.invalid", PasswordHash: "hash", Role: "operator"}); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateSession("session-"+suffix, userID); err != nil {
		t.Fatal(err)
	}
	if user, err := st.FindUserBySession("session-" + suffix); err != nil || user == nil || user.ID != userID {
		t.Fatalf("session lookup: %+v %v", user, err)
	}
	if err := st.InsertCustomer(Customer{ID: customerID, TenantID: tenantID, SyntheticName: "Synthetic", SyntheticContactRef: "fixture"}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertOrder(Order{ID: "order-" + suffix, TenantID: tenantID, CustomerID: customerID, OrderNo: orderNo, Item: "Fixture", OrderedAt: "2026-11-14", DeliveryStatus: "shipped"}); err != nil {
		t.Fatal(err)
	}
	if order, err := st.FindOrderByNo(tenantID, orderNo); err != nil || order == nil || order.CustomerName != "Synthetic" {
		t.Fatalf("order lookup: %+v %v", order, err)
	}
	inq, err := st.InsertInquiry(Inquiry{ID: inquiryID, TenantID: tenantID, Subject: "Contract", RedactedBody: "body", NormalizedBodyHash: "hash"})
	if err != nil || inq == nil {
		t.Fatalf("insert inquiry: %+v %v", inq, err)
	}
	if err := st.InsertArticle(Article{ID: articleID, TenantID: tenantID, Title: "Policy", IntentTags: []string{"other"}, Body: "Evidence"}, "evidence", "test-index"); err != nil {
		t.Fatal(err)
	}
	newTitle := "Updated Policy"
	if article, err := st.UpdateArticle(tenantID, articleID, ArticlePatch{Title: &newTitle}); err != nil || article == nil || article.Title != newTitle {
		t.Fatalf("update article: %+v %v", article, err)
	}

	run, err := st.CreateRun(Run{ID: "run-" + suffix, TenantID: tenantID, InquiryID: inquiryID, IdempotencyKey: strp("run-idem-" + suffix)})
	if err != nil || run == nil {
		t.Fatalf("create run: %+v %v", run, err)
	}
	claimed, err := st.ClaimNextRun("worker", 60000)
	if err != nil || claimed == nil || claimed.ID != run.ID {
		t.Fatalf("claim run: %+v %v", claimed, err)
	}
	output := `{"intent":"other"}`
	if err := st.SaveCheckpoint(CheckpointInput{TenantID: tenantID, RunID: run.ID, StepKey: "classify", Attempt: 1, State: "completed", OutputRedacted: &output}); err != nil {
		t.Fatal(err)
	}
	if calls, err := st.InsertToolCall(ToolCall{TenantID: tenantID, RunID: &run.ID, Server: "kb", Tool: "search", RiskLevel: "read", Status: "completed"}); err != nil || calls == "" {
		t.Fatalf("tool call: %q %v", calls, err)
	}

	draft, err := st.InsertDraft(Draft{ID: "draft-" + suffix, TenantID: tenantID, RunID: run.ID, Revision: 1, Body: "Draft", CitedArticleIDs: `["` + articleID + `"]`, FactualReferences: `[]`, ContentHash: "draft-hash"})
	if err != nil || draft == nil {
		t.Fatalf("insert draft: %+v %v", draft, err)
	}
	action, err := st.InsertProposedAction(ProposedAction{ID: "action-" + suffix, TenantID: tenantID, DraftID: draft.ID, RunID: &run.ID, ActionType: "send_demo_reply", CanonicalPayload: `{"body":"Draft"}`, PayloadHash: "payload-hash"})
	if err != nil || action == nil {
		t.Fatalf("insert action: %+v %v", action, err)
	}
	if err := st.InsertApproval(Approval{ID: "approval-" + suffix, TenantID: tenantID, DraftID: draft.ID, DraftRevision: 1, DraftHash: draft.ContentHash, OperatorID: userID, SelectedActionIDs: `["` + action.ID + `"]`, PolicyVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	if approval, err := st.GetApprovalForAction(action.ID); err != nil || approval == nil {
		t.Fatalf("approval lookup: %+v %v", approval, err)
	}
	job, err := st.InsertExecutionJob(ExecutionJob{ID: "job-" + suffix, TenantID: tenantID, ProposedActionID: action.ID, IdempotencyKey: "job-idem-" + suffix})
	if err != nil || job == nil {
		t.Fatalf("insert job: %+v %v", job, err)
	}
	claimedJob, err := st.ClaimNextJob("executor", 60000)
	if err != nil || claimedJob == nil || claimedJob.ID != job.ID {
		t.Fatalf("claim job: %+v %v", claimedJob, err)
	}

	ticket, err := st.InsertTicket(Ticket{ID: "ticket-" + suffix, TenantID: tenantID, InquiryID: inquiryID, SourceActionID: &action.ID})
	if err != nil || ticket == nil {
		t.Fatalf("insert ticket: %+v %v", ticket, err)
	}
	if _, err := st.InsertTicketNote(TicketNote{ID: "note-" + suffix, TenantID: tenantID, TicketID: ticket.ID, Body: "note", SourceActionID: &action.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.InsertOutbox(OutboxMessage{ID: "outbox-" + suffix, TenantID: tenantID, InquiryID: inquiryID, DraftRevision: 1, Body: "Draft", SourceActionID: &action.ID}); err != nil {
		t.Fatal(err)
	}
	result := `{"ok":true}`
	if err := st.FinishJob(job.ID, "succeeded", &result, nil); err != nil {
		t.Fatal(err)
	}
	if err := st.FinishRun(run.ID, FinishRunPatch{State: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := st.InsertAudit(AuditEvent{TenantID: tenantID, ActorType: "system", Action: "contract.completed", EntityType: "run", EntityID: run.ID}); err != nil {
		t.Fatal(err)
	}
	if events, err := st.ListAudit(tenantID, "run", run.ID); err != nil || len(events) != 1 {
		t.Fatalf("audit: %d %v", len(events), err)
	}
	if err := st.InsertEvalRun("eval-"+suffix, EvalRunMeta{}, `{"ok":true}`); err != nil {
		t.Fatal(err)
	}
}
