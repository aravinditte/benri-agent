package agent

import (
	"strings"

	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
)

// Deterministic policy engine (SPEC §5.1 step 4), ported from v2. Runs
// BEFORE drafting. Hard rules only; no model involvement.

type PolicyInput struct {
	Text             string
	Classification   Classification
	EvidenceCount    int
	OrderRequired    bool
	OrderFound       bool
	OrderHintPresent bool
	OrderAmbiguous   bool
	EvidenceTexts    []string
}

type PolicyDecision struct {
	Escalate bool   `json:"escalate"`
	Reason   string `json:"reason,omitempty"`
	NoteJP   string `json:"note_jp,omitempty"`
}

var orderRequiredIntents = map[domain.Intent]bool{
	domain.IntentDeliveryStatus: true, domain.IntentCancellation: true, domain.IntentReturnExchange: true,
}

func EvaluatePolicy(in PolicyInput) PolicyDecision {
	text := japanese.Normalize(in.Text)
	if HasAny(text, LegalSafetyKeywords) {
		return PolicyDecision{true, domain.EscLegalSafety, "法務・安全に関わる可能性があるため、担当者へ引き継ぎます。"}
	}
	if HasAny(text, IdentityKeywords) || HasAny(text, PIIFishingPatterns) || HasAny(text, CredentialDisclosurePatterns) {
		return PolicyDecision{true, domain.EscIdentity, "本人確認・個人情報に関わる依頼のため、担当者へ引き継ぎます。"}
	}
	if in.Classification.Intent == domain.IntentComplaint || HasAny(text, ComplaintKeywords) {
		return PolicyDecision{true, domain.EscComplaint, "クレーム対応は担当者が直接対応します。顧客向け返信案は作成しません。"}
	}
	if HasAny(text, InjectionPatterns) || HasAny(text, UnsupportedRequestPatterns) {
		return PolicyDecision{true, domain.EscPolicyNotCover, "ポリシーで定められていない要求が含まれているため、担当者へ引き継ぎます。"}
	}
	if strings.Contains(text, "海外") {
		covered := false
		for _, t := range in.EvidenceTexts {
			if strings.Contains(t, "海外") {
				covered = true
			}
		}
		if !covered {
			return PolicyDecision{true, domain.EscNoEvidence, "海外配送など、対応範囲外の内容については根拠となるナレッジがありません。"}
		}
	}
	if in.EvidenceCount == 0 {
		return PolicyDecision{true, domain.EscNoEvidence, "回答の根拠となるナレッジが見つかりませんでした。"}
	}
	if in.OrderAmbiguous {
		return PolicyDecision{true, domain.EscOrderAmbiguous, "複数のご注文番号が示されるなど、対象の注文を特定できませんでした。"}
	}
	if in.OrderHintPresent && orderRequiredIntents[in.Classification.Intent] && !in.OrderFound {
		return PolicyDecision{true, domain.EscOrderAmbiguous, "ご注文番号に一致する注文が確認できませんでした。"}
	}
	if in.Classification.Confidence < 0.6 {
		return PolicyDecision{true, domain.EscLowConfidence, "意図を十分な確度で判定できませんでした。"}
	}
	return PolicyDecision{Escalate: false}
}
