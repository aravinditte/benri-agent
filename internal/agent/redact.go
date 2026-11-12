package agent

import (
	"regexp"
	"strings"

	"github.com/aravinditte/benri-agent/internal/japanese"
)

// PII redaction (SPEC §5.1 step 0, §6.4). NFKC first, then pattern detectors.
// This is a LIMITED detector for common Japanese patterns — not proof of
// complete PII removal. Only the redacted body is ever persisted.
// Pattern ORDER is load-bearing (v2 correction): the fixture order-number
// pattern runs BEFORE the postal pattern so HIN-2026-0001 is masked as
// [注文番号], not mangled as a postal code.
type pattern struct {
	re          *regexp.Regexp
	replacement string
	kind        string
}

var patterns = []pattern{
	{regexp.MustCompile(`[A-Z]{3}-\d{4}-\d{4}`), "[注文番号]", "order_no"},
	{regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`), "[メールアドレス]", "email"},
	{regexp.MustCompile(`0\d{1,4}-\d{1,4}-\d{4}`), "[電話番号]", "phone_hyphen"},
	{regexp.MustCompile(`0\d{9,10}`), "[電話番号]", "phone_plain"},
	{regexp.MustCompile(`〒\s?\d{3}-\d{4}`), "[郵便番号]", "postal_marked"},
	{regexp.MustCompile(`(^|[^\d-])\d{3}-\d{4}($|[^\d-])`), "[郵便番号]", "postal"},
	{regexp.MustCompile(`(^|\D)\d{4}\s?\d{4}\s?\d{4}($|\D)`), "[番号]", "my_number_like"},
}

type RedactionResult struct {
	Text  string
	Kinds []string
}

func RedactPII(input string) RedactionResult {
	text := japanese.Normalize(input)
	kinds := []string{}
	for _, p := range patterns {
		if p.re.MatchString(text) {
			kinds = append(kinds, p.kind)
		}
		if p.kind == "postal" || p.kind == "my_number_like" {
			// Preserve the boundary characters consumed by the lookaround-free pattern.
			text = p.re.ReplaceAllStringFunc(text, func(m string) string {
				// keep leading/trailing non-digit boundary chars
				start := 0
				for start < len(m) && !(m[start] >= '0' && m[start] <= '9') {
					start++
				}
				end := len(m)
				for end > start && !(m[end-1] >= '0' && m[end-1] <= '9') {
					end--
				}
				prefix := ""
				if start > 0 {
					prefix = m[:start]
					// The synthetic prefix group may include a Japanese char boundary; keep only ascii/space boundary
					if strings.ContainsAny(prefix, "0123456789") {
						prefix = ""
					}
				}
				suffix := ""
				if end < len(m) {
					suffix = m[end:]
				}
				return prefix + p.replacement + suffix
			})
		} else {
			text = p.re.ReplaceAllString(text, p.replacement)
		}
	}
	return RedactionResult{Text: text, Kinds: kinds}
}

func MaskOrderNo(orderNo string) string {
	r := []rune(orderNo)
	if len(r) <= 4 {
		return "****"
	}
	return "****" + string(r[len(r)-4:])
}
