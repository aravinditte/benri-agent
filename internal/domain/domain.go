// Package domain holds Benri's fixed taxonomy, stable error codes and shared
// domain types (SPEC v3 §1.2, §4.5, §5.3). Ported from the v2 TypeScript
// implementation; semantics are unchanged.
package domain

import "fmt"

// Intent is one of the fixed inquiry categories (SPEC §1.2).
type Intent string

const (
	IntentDeliveryStatus Intent = "delivery_status"
	IntentReturnExchange Intent = "return_exchange"
	IntentInvoiceRequest Intent = "invoice_request"
	IntentProductUsage   Intent = "product_usage"
	IntentCancellation   Intent = "cancellation"
	IntentPriceStock     Intent = "price_stock"
	IntentComplaint      Intent = "complaint"
	IntentAccount        Intent = "account"
	IntentOther          Intent = "other"
)

var Intents = []Intent{IntentDeliveryStatus, IntentReturnExchange, IntentInvoiceRequest, IntentProductUsage, IntentCancellation, IntentPriceStock, IntentComplaint, IntentAccount, IntentOther}

var IntentLabelsJP = map[Intent]string{
	IntentDeliveryStatus: "配送状況", IntentReturnExchange: "返品・交換", IntentInvoiceRequest: "請求書",
	IntentProductUsage: "使い方", IntentCancellation: "解約・キャンセル", IntentPriceStock: "価格・在庫",
	IntentComplaint: "クレーム", IntentAccount: "アカウント", IntentOther: "その他",
}

func ValidIntent(s string) bool {
	for _, i := range Intents {
		if string(i) == s {
			return true
		}
	}
	return false
}

// Escalation reason codes (SPEC §4.5) — stable machine-readable strings.
const (
	EscComplaint      = "COMPLAINT_REQUIRES_HUMAN"
	EscNoEvidence     = "NO_RELEVANT_KB_EVIDENCE"
	EscLowConfidence  = "LOW_CLASSIFICATION_CONFIDENCE"
	EscOrderAmbiguous = "ORDER_ID_AMBIGUOUS"
	EscPolicyNotCover = "POLICY_NOT_COVERED"
	EscIdentity       = "IDENTITY_SENSITIVE_REQUEST"
	EscLegalSafety    = "LEGAL_OR_SAFETY_SENSITIVE"
	EscVerifyFailed   = "VERIFICATION_FAILED"
	EscBudgetExceeded = "MODEL_BUDGET_EXCEEDED"
)

const (
	PromptVersion  = "benri-prompt-v3.0.0"
	PolicyVersion  = "benri-policy-v1.0.0"
	DatasetVersion = "1.0.0"
)

// Error is a typed error with a stable machine-readable code (SPEC §5.3).
type Error struct {
	Code string
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }
func NewError(code, format string, args ...any) *Error {
	return &Error{Code: code, Msg: fmt.Sprintf(format, args...)}
}
func CodeOf(err error) string {
	if e, ok := err.(*Error); ok {
		return e.Code
	}
	return ""
}

// Limits are the hard per-run limits from configuration (SPEC §5.3).
type Limits struct {
	MaxModelCallsPerRun int
	MaxToolCallsPerRun  int
	MaxRunDurationMS    int
	MaxInputTokens      int
	MaxOutputTokens     int
	MaxCostPerRunJPY    *float64
}
