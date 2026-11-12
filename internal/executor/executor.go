// Package executor runs APPROVED execution jobs only (SPEC §5.2). In v3 it
// holds an "executor"-role store handle: it can claim/finish jobs and
// insert outbox records, but it CANNOT insert tickets directly — ticket
// creation happens only inside the ticket MCP server after it loads the
// persisted approval and validates the payload hash (SPEC §9).
package executor

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/store"
)

// TicketGateway is the ONLY route to ticket writes.
type TicketGateway interface {
	CreateTicket(ctx context.Context, approvedActionID string) (ticketID string, replayed bool, err error)
	AddTicketNote(ctx context.Context, approvedActionID string) (ticketID string, err error)
}

// OutcomeUnknownError marks an ambiguous tool outcome (SPEC §3.4: never
// blind-retry; the job becomes `unknown`).
type OutcomeUnknownError struct{ Msg string }

func (e *OutcomeUnknownError) Error() string { return "OUTCOME_UNKNOWN: " + e.Msg }

func IsOutcomeUnknown(err error) bool {
	var target *OutcomeUnknownError
	return errors.As(err, &target)
}

type Outcome struct {
	JobID  string `json:"job_id"`
	Status string `json:"status"` // succeeded | failed | unknown
	Detail string `json:"detail,omitempty"`
	Replay bool   `json:"replay,omitempty"`
}

// LoadVerifiedAction is the executor/ticket-server pre-flight check: the
// action exists, its payload hash matches, and a persisted approval covers
// it for the current draft revision+hash.
func LoadVerifiedAction(st store.Repository, actionID string) (*store.ProposedAction, string) {
	action, err := st.GetActionByID(actionID)
	if err != nil || action == nil {
		return nil, "action not found"
	}
	hash, err := agent.HashOfPayloadJSON(action.CanonicalPayload)
	if err != nil || hash != action.PayloadHash {
		return nil, "payload hash mismatch"
	}
	draft, err := st.GetDraft(action.TenantID, action.DraftID)
	if err != nil || draft == nil {
		return nil, "draft not found"
	}
	approval, err := st.GetApprovalForAction(actionID)
	if err != nil || approval == nil || approval.RejectedReason != nil {
		return nil, "no persisted approval for this action"
	}
	if approval.DraftRevision != draft.Revision || approval.DraftHash != draft.ContentHash {
		return nil, "approval does not cover the current draft revision/hash"
	}
	if action.State == "proposed" || action.State == "rejected" {
		return nil, "action was not approved"
	}
	return action, ""
}

// ExecuteClaimedJob executes a job already claimed by the worker loop
// (state=running). Ambiguous outcomes mark the job `unknown` and are never
// blind-retried.
func ExecuteClaimedJob(ctx context.Context, st store.Repository, job *store.ExecutionJob, tickets TicketGateway) (*Outcome, error) {
	if job.State == "succeeded" {
		return &Outcome{JobID: job.ID, Status: "succeeded", Detail: "job already succeeded (idempotent)", Replay: true}, nil
	}
	if job.State == "unknown" {
		return &Outcome{JobID: job.ID, Status: "unknown", Detail: "job outcome is ambiguous; manual reconciliation required"}, nil
	}
	action, err := st.GetActionByID(job.ProposedActionID)
	if err != nil || action == nil {
		_ = st.FinishJob(job.ID, "failed", nil, strp("ACTION_RECORD_MISSING"))
		return &Outcome{JobID: job.ID, Status: "failed", Detail: "approved action record missing"}, nil
	}
	if action.State == "succeeded" {
		_ = st.FinishJob(job.ID, "succeeded", nil, nil)
		return &Outcome{JobID: job.ID, Status: "succeeded", Detail: "action already executed (idempotent replay)", Replay: true}, nil
	}
	verified, detail := LoadVerifiedAction(st, action.ID)
	if verified == nil {
		code := "APPROVAL_VALIDATION_FAILED"
		_ = st.FinishJob(job.ID, "failed", nil, &code)
		return &Outcome{JobID: job.ID, Status: "failed", Detail: detail}, nil
	}
	payload, err := agent.ParsePayload(action.CanonicalPayload)
	if err != nil {
		_ = st.FinishJob(job.ID, "failed", nil, strp("PAYLOAD_UNPARSABLE"))
		return &Outcome{JobID: job.ID, Status: "failed", Detail: "canonical payload unparsable"}, nil
	}
	// State ownership (v3): for send_demo_reply the executor owns the
	// action transition (queued->running->succeeded, below). For ticket
	// actions the TICKET MCP SERVER owns the transition — it performs the
	// atomic queued->running claim and marks succeeded after inserting.
	// The executor must not pre-transition ticket actions or the tool's
	// claim would fail.
	isTicketAction := payload.ActionType == "create_ticket" || payload.ActionType == "add_ticket_note"
	if !isTicketAction {
		won, _ := st.TransitionAction(action.ID, "queued", "running")
		if !won {
			cur, _ := st.GetActionByID(action.ID)
			if cur != nil && cur.State == "succeeded" {
				_ = st.FinishJob(job.ID, "succeeded", nil, nil)
				return &Outcome{JobID: job.ID, Status: "succeeded", Detail: "action already executed (idempotent replay)", Replay: true}, nil
			}
			_ = st.FinishJob(job.ID, "failed", nil, strp("ACTION_TRANSITION_LOST"))
			return &Outcome{JobID: job.ID, Status: "failed", Detail: "action transition lost"}, nil
		}
	}
	failJob := func(code, detail string) (*Outcome, error) {
		if !isTicketAction {
			_ = st.SetActionState(action.ID, "failed")
		}
		_ = st.FinishJob(job.ID, "failed", nil, &code)
		return &Outcome{JobID: job.ID, Status: "failed", Detail: detail}, nil
	}
	unknownJob := func(detail string) (*Outcome, error) {
		// Action stays `running`: the outcome is genuinely ambiguous.
		_ = st.FinishJob(job.ID, "unknown", nil, strp("OUTCOME_UNKNOWN"))
		return &Outcome{JobID: job.ID, Status: "unknown", Detail: detail}, nil
	}
	succeedJob := func(replay bool, result any) (*Outcome, error) {
		if !isTicketAction {
			_ = st.SetActionState(action.ID, "succeeded")
		}
		var rj *string
		if result != nil {
			b, _ := json.Marshal(result)
			s := string(b)
			rj = &s
		}
		_ = st.FinishJob(job.ID, "succeeded", rj, nil)
		return &Outcome{JobID: job.ID, Status: "succeeded", Replay: replay}, nil
	}

	switch payload.ActionType {
	case "send_demo_reply":
		if existing, _ := st.FindOutboxByAction(action.ID); existing != nil {
			return succeedJob(true, map[string]string{"outbox_id": existing.ID, "note": "idempotent replay"})
		}
		body, _ := payload.Arguments["body"].(string)
		msg, err := st.InsertOutbox(store.OutboxMessage{
			ID: store.NewID("outbox"), TenantID: action.TenantID, InquiryID: payload.InquiryID,
			DraftRevision: payload.DraftRevision, Body: body, DeliveryState: "queued_demo",
			SourceActionID: &action.ID,
		})
		if err != nil {
			if cerr, ok := err.(*domain.Error); ok && cerr.Code == "UNIQUE_CONSTRAINT" {
				return succeedJob(true, nil)
			}
			if _, ok := err.(*store.CapabilityError); ok {
				return failJob("CAPABILITY_DENIED", err.Error())
			}
			return nil, err
		}
		return succeedJob(false, map[string]string{"outbox_id": msg.ID})
	case "create_ticket":
		ticketID, replayed, err := tickets.CreateTicket(ctx, action.ID)
		if err != nil {
			if IsOutcomeUnknown(err) {
				return unknownJob("ticket outcome ambiguous; job marked unknown and NOT retried")
			}
			return failJob("TICKET_CREATE_FAILED", err.Error())
		}
		return succeedJob(replayed, map[string]string{"ticket_id": ticketID})
	case "add_ticket_note":
		ticketID, err := tickets.AddTicketNote(ctx, action.ID)
		if err != nil {
			if IsOutcomeUnknown(err) {
				return unknownJob("note outcome ambiguous; job marked unknown and NOT retried")
			}
			return failJob("TICKET_NOTE_FAILED", err.Error())
		}
		return succeedJob(false, map[string]string{"ticket_id": ticketID})
	default:
		return failJob("UNKNOWN_ACTION_TYPE", "unknown action type "+payload.ActionType)
	}
}

func strp(s string) *string { return &s }
