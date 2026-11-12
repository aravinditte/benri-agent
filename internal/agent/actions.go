package agent

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/domain"
)

// Proposed actions come from deterministic intent/policy templates ONLY
// (SPEC §5.1, §8.5). The model never proposes or parameterizes actions.

var actionTemplates = map[domain.Intent][]string{
	domain.IntentDeliveryStatus: {"send_demo_reply"},
	domain.IntentProductUsage:   {"send_demo_reply"},
	domain.IntentPriceStock:     {"send_demo_reply"},
	domain.IntentAccount:        {"send_demo_reply"},
	domain.IntentOther:          {"send_demo_reply"},
	domain.IntentInvoiceRequest: {"send_demo_reply", "create_ticket"},
	domain.IntentReturnExchange: {"send_demo_reply", "create_ticket", "add_ticket_note"},
	domain.IntentCancellation:   {"send_demo_reply", "create_ticket"},
}

func ActionTypesForIntent(intent domain.Intent) []string {
	if t, ok := actionTemplates[intent]; ok {
		return t
	}
	return []string{"send_demo_reply"}
}

// StableStringify is the canonical JSON encoding used for payload hashing
// (v2 semantics: sorted object keys, no insignificant whitespace).
func StableStringify(v any) string {
	var sb strings.Builder
	writeStable(&sb, v)
	return sb.String()
}
func writeStable(sb *strings.Builder, v any) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteString("{")
		for i, k := range keys {
			if i > 0 {
				sb.WriteString(",")
			}
			kb, _ := json.Marshal(k)
			sb.Write(kb)
			sb.WriteString(":")
			writeStable(sb, t[k])
		}
		sb.WriteString("}")
	case []any:
		sb.WriteString("[")
		for i, e := range t {
			if i > 0 {
				sb.WriteString(",")
			}
			writeStable(sb, e)
		}
		sb.WriteString("]")
	default:
		b, _ := json.Marshal(v)
		sb.Write(b)
	}
}

type CanonicalPayload struct {
	TenantID      string         `json:"tenant_id"`
	InquiryID     string         `json:"inquiry_id"`
	DraftID       string         `json:"draft_id"`
	DraftRevision int            `json:"draft_revision"`
	ActionType    string         `json:"action_type"`
	Target        string         `json:"target"`
	Arguments     map[string]any `json:"arguments"`
}

func (p CanonicalPayload) Map() map[string]any {
	return map[string]any{
		"tenant_id": p.TenantID, "inquiry_id": p.InquiryID, "draft_id": p.DraftID,
		"draft_revision": float64(p.DraftRevision), "action_type": p.ActionType,
		"target": p.Target, "arguments": p.Arguments,
	}
}

func BuildCanonicalPayload(tenantID, inquiryID, draftID string, draftRevision int, actionType, draftBody, subject, urgency string) CanonicalPayload {
	base := CanonicalPayload{TenantID: tenantID, InquiryID: inquiryID, DraftID: draftID, DraftRevision: draftRevision, ActionType: actionType}
	switch actionType {
	case "send_demo_reply":
		base.Target = "demo_outbox"
		base.Arguments = map[string]any{"body": draftBody, "draft_revision": float64(draftRevision)}
	case "create_ticket":
		base.Target = "demo_tickets"
		priority := "normal"
		if urgency == "high" {
			priority = "high"
		}
		base.Arguments = map[string]any{"subject": subject, "priority": priority}
	case "add_ticket_note":
		base.Target = "demo_tickets"
		base.Arguments = map[string]any{"note": "オペレーター確認依頼: 承認済み対応内容に沿ってご対応をお願いいたします。"}
	}
	return base
}

func PayloadHash(p CanonicalPayload) string {
	return auth.ContentHash(StableStringify(p.Map()))
}

// ParsePayload decodes a persisted canonical payload JSON string.
func ParsePayload(s string) (CanonicalPayload, error) {
	var p CanonicalPayload
	err := json.Unmarshal([]byte(s), &p)
	return p, err
}

// HashOfPayloadJSON recomputes the hash from the persisted JSON (used by the
// executor/ticket tool to verify the stored payload matches its hash).
func HashOfPayloadJSON(s string) (string, error) {
	var generic map[string]any
	if err := json.Unmarshal([]byte(s), &generic); err != nil {
		return "", err
	}
	return auth.ContentHash(StableStringify(generic)), nil
}
