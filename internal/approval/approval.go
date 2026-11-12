// Package approval implements SPEC §5.2/§8.6: human approval binds an exact
// draft revision and exact per-action payload hashes, re-verifies the draft
// before approval, and in ONE transaction creates the approval + execution
// jobs. The API process performs this with an "api"-role store handle:
// it can insert approvals and execution jobs but cannot write tickets or
// outbox records (SPEC §9, v3 finer boundary).
package approval

import (
	"encoding/json"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/verify"
)

type ReverifyFn func(draft store.Draft, runID string) (bool, string)

// ReverifyDraft rebuilds the verification context from checkpoints (retrieve
// evidence + CRM order) and re-runs the deterministic verifier.
func ReverifyDraft(st store.Repository, draft store.Draft, runID string) (bool, string) {
	evidence := []verify.EvidenceItem{}
	var order *verify.OrderContext
	orderText := ""
	cps, err := st.GetCheckpoints(draft.TenantID, runID)
	if err != nil {
		return false, "checkpoint read failed"
	}
	for _, cp := range cps {
		if cp.OutputRedacted == nil {
			continue
		}
		switch cp.StepKey {
		case "retrieve":
			var wrap struct {
				Hits []struct {
					ArticleID string `json:"article_id"`
				} `json:"hits"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			for _, h := range wrap.Hits {
				if art, err := st.GetArticle(draft.TenantID, h.ArticleID); err == nil && art != nil {
					evidence = append(evidence, verify.EvidenceItem{ArticleID: art.ID, Text: art.Body})
				}
			}
		case "lookup_order":
			var wrap struct {
				Order *struct {
					Item           string `json:"item"`
					DeliveryStatus string `json:"delivery_status"`
				} `json:"order"`
			}
			_ = json.Unmarshal([]byte(*cp.OutputRedacted), &wrap)
			if wrap.Order != nil {
				order = &verify.OrderContext{DeliveryStatus: wrap.Order.DeliveryStatus, Item: wrap.Order.Item}
				orderText = wrap.Order.Item + " " + wrap.Order.DeliveryStatus
			}
		}
	}
	var cited []string
	_ = json.Unmarshal([]byte(draft.CitedArticleIDs), &cited)
	var agentRefs []agent.FactualReference
	_ = json.Unmarshal([]byte(draft.FactualReferences), &agentRefs)
	refs := make([]verify.FactualReference, 0, len(agentRefs))
	for _, r := range agentRefs {
		refs = append(refs, verify.FactualReference{FactKey: r.FactKey, NormalizedValue: r.NormalizedValue, SourceArticleID: r.SourceArticleID, SourceChunkID: r.SourceChunkID})
	}
	articleTenant := map[string]string{}
	if arts, err := st.ListArticles(draft.TenantID); err == nil {
		for _, a := range arts {
			articleTenant[a.ID] = draft.TenantID
		}
	}
	res := verify.VerifyDraft(verify.DraftLike{Body: draft.Body, CitedArticleIDs: cited, FactualReferences: refs},
		verify.Context{TenantID: draft.TenantID, Evidence: evidence, ArticleTenant: articleTenant, Order: order, OrderText: orderText})
	if !res.Passed {
		return false, "draft no longer passes verification"
	}
	return true, ""
}

// Approve binds approval to the exact draft revision + per-action payload
// hashes. On ANY mismatch nothing is approved and nothing is enqueued.
// The mutation sequence runs inside one store transaction.
func Approve(st store.Repository, tenantID, draftID string, expectedRevision int, expectedDraftHash string, selected []store.SelectedAction, actorID string, reverify ReverifyFn) (*store.Draft, error) {
	draft, err := st.GetDraft(tenantID, draftID)
	if err != nil {
		return nil, err
	}
	if draft == nil {
		return nil, domain.NewError("DRAFT_NOT_FOUND", "approval target does not exist")
	}
	if draft.Revision != expectedRevision {
		return nil, domain.NewError("REVISION_MISMATCH", "draft revision changed since review")
	}
	if draft.State != "pending_review" {
		return nil, domain.NewError("DRAFT_NOT_PENDING", "draft is %s", draft.State)
	}
	if draft.ContentHash != expectedDraftHash {
		return nil, domain.NewError("DRAFT_HASH_MISMATCH", "draft content changed since review")
	}
	if reverify != nil {
		if ok, detail := reverify(*draft, draft.RunID); !ok {
			return nil, domain.NewError("REVERIFICATION_FAILED", "%s", detail)
		}
	}
	proposed, err := st.ListActionsByDraft(tenantID, draftID)
	if err != nil {
		return nil, err
	}
	byID := map[string]*store.ProposedAction{}
	for i := range proposed {
		byID[proposed[i].ID] = &proposed[i]
	}
	for _, sel := range selected {
		action, ok := byID[sel.ActionID]
		if !ok {
			return nil, domain.NewError("ACTION_NOT_FOUND", "action %s not on this draft", sel.ActionID)
		}
		if action.DraftID != draftID {
			return nil, domain.NewError("ACTION_DRAFT_MISMATCH", "action %s belongs to another draft", sel.ActionID)
		}
		if action.State != "proposed" {
			return nil, domain.NewError("ACTION_NOT_PROPOSED", "action %s already decided", sel.ActionID)
		}
		if action.PayloadHash != sel.PayloadHash {
			return nil, domain.NewError("ACTION_HASH_MISMATCH", "action %s payload changed since display", sel.ActionID)
		}
		payload, err := agent.ParsePayload(action.CanonicalPayload)
		if err != nil || payload.DraftRevision != expectedRevision || payload.DraftID != draftID {
			return nil, domain.NewError("ACTION_REVISION_MISMATCH", "action %s is bound to another revision", sel.ActionID)
		}
	}
	selectedIDs := make([]string, 0, len(selected))
	for _, sel := range selected {
		selectedIDs = append(selectedIDs, sel.ActionID)
	}
	selectedJSON, _ := json.Marshal(selectedIDs)
	err = st.Transaction(func(tx store.Repository) error {
		if err := tx.SetDraftState(tenantID, draftID, "approved"); err != nil {
			return err
		}
		if err := tx.InsertApproval(store.Approval{
			ID: store.NewID("approval"), TenantID: tenantID, DraftID: draftID,
			DraftRevision: expectedRevision, DraftHash: expectedDraftHash,
			OperatorID: actorID, SelectedActionIDs: string(selectedJSON),
			PolicyVersion: domain.PolicyVersion,
		}); err != nil {
			return err
		}
		for _, sel := range selected {
			action := byID[sel.ActionID]
			if err := tx.SetActionState(action.ID, "approved"); err != nil {
				return err
			}
			if _, err := tx.InsertExecutionJob(store.ExecutionJob{
				ID: store.NewID("job"), TenantID: tenantID, ProposedActionID: action.ID,
				IdempotencyKey: "exec:" + action.ID + ":" + action.PayloadHash,
			}); err != nil {
				return err
			}
			if err := tx.SetActionState(action.ID, "queued"); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	auditHash := auth.ContentHash(expectedDraftHash)
	_ = st.InsertAudit(store.AuditEvent{TenantID: tenantID, ActorType: "user", ActorID: &actorID, Action: "draft.approved", EntityType: "draft", EntityID: draftID, AfterHash: &auditHash})
	return st.GetDraft(tenantID, draftID)
}

// Reject records a rejection; no execution jobs are created.
func Reject(st store.Repository, tenantID, draftID, actorID, reason string) (*store.Draft, error) {
	draft, err := st.GetDraft(tenantID, draftID)
	if err != nil {
		return nil, err
	}
	if draft == nil {
		return nil, domain.NewError("DRAFT_NOT_FOUND", "draft does not exist")
	}
	if draft.State != "pending_review" {
		return nil, domain.NewError("DRAFT_NOT_PENDING", "draft is %s", draft.State)
	}
	err = st.Transaction(func(tx store.Repository) error {
		if err := tx.SetDraftState(tenantID, draftID, "rejected"); err != nil {
			return err
		}
		actions, _ := tx.ListActionsByDraft(tenantID, draftID)
		for i := range actions {
			if actions[i].State == "proposed" {
				if err := tx.SetActionState(actions[i].ID, "rejected"); err != nil {
					return err
				}
			}
		}
		rsn := reason
		return tx.InsertApproval(store.Approval{
			ID: store.NewID("approval"), TenantID: tenantID, DraftID: draftID,
			DraftRevision: draft.Revision, DraftHash: draft.ContentHash,
			OperatorID: actorID, SelectedActionIDs: "[]",
			PolicyVersion: domain.PolicyVersion, RejectedReason: &rsn,
		})
	})
	if err != nil {
		return nil, err
	}
	return st.GetDraft(tenantID, draftID)
}
