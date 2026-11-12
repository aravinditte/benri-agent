package agent_test

import (
	"strings"
	"testing"

	"github.com/aravinditte/benri-agent/internal/agent"
)

func TestRedactPhonePostalEmailAfterNFKC(t *testing.T) {
	in := "電話は０９０-１２３４-５６７８、郵便番号は〒100-0001、メールは test@example.jp です"
	out := agent.RedactPII(in).Text
	if strings.Contains(out, "090-1234-5678") || strings.Contains(out, "０９０") {
		t.Errorf("phone not redacted: %s", out)
	}
	if strings.Contains(out, "100-0001") {
		t.Errorf("postal not redacted: %s", out)
	}
	if strings.Contains(out, "test@example.jp") {
		t.Errorf("email not redacted: %s", out)
	}
}

func TestRedactOrderNumberBeforePostal(t *testing.T) {
	// Load-bearing order: the order-number pattern must run BEFORE the
	// postal pattern, or HIN-2026-0001 would be mangled (v2 correction).
	out := agent.RedactPII("注文番号はHIN-2026-0001です").Text
	if !strings.Contains(out, "[注文番号]") {
		t.Errorf("order number not masked as a unit: %s", out)
	}
	if strings.Contains(out, "2026") {
		t.Errorf("order number partially leaked: %s", out)
	}
}

func TestRedactKeepsNonPIIText(t *testing.T) {
	in := "配送状況を教えてください。注文してから3日経ちました。"
	out := agent.RedactPII(in).Text
	if out != in {
		t.Errorf("non-PII text changed: %q -> %q", in, out)
	}
}
