// Package verify implements the deterministic draft verifier (SPEC §8.5),
// ported from v2: schema constraints, citation existence/tenant/evidence
// membership, factual-reference value matching, policy-sensitive values,
// signature and banned-phrase/style rules.
// LIMIT: this does NOT prove semantic entailment of free-form sentences.
// Human review remains mandatory.
package verify

import (
	"strings"

	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/store"
)

// DraftLike is the verifier's view of a draft (mirrors agent.DraftOutput).
type DraftLike struct {
	Body              string
	CitedArticleIDs   []string
	FactualReferences []FactualReference
}

type FactualReference struct {
	FactKey         string
	NormalizedValue string
	SourceArticleID string
	SourceChunkID   string
}

type EvidenceItem struct {
	ArticleID string
	Text      string
}

type OrderContext struct {
	DeliveryStatus string
	Item           string
}

type Check struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
	Detail string `json:"detail,omitempty"`
}

type Result struct {
	Passed          bool    `json:"passed"`
	Checks          []Check `json:"checks"`
	VerifiedAt      string  `json:"verified_at"`
	VerifierVersion string  `json:"verifier_version"`
}

type Context struct {
	TenantID      string
	Evidence      []EvidenceItem
	ArticleTenant map[string]string
	Order         *OrderContext
	OrderText     string
}

func VerifyDraft(draft DraftLike, c Context) Result {
	checks := []Check{}
	push := func(name string, passed bool, detail string) {
		checks = append(checks, Check{Name: name, Passed: passed, Detail: detail})
	}
	result := func() Result {
		passed := true
		for _, ch := range checks {
			if !ch.Passed {
				passed = false
			}
		}
		return Result{Passed: passed, Checks: checks, VerifiedAt: store.NowISO(), VerifierVersion: "verifier-v1"}
	}
	// 1. schema + size constraints
	schemaOK := len(draft.Body) >= 1 && len([]rune(draft.Body)) <= 4000 && len(draft.CitedArticleIDs) >= 1
	if !schemaOK {
		push("schema", false, "draft output failed schema validation")
		return result()
	}
	push("schema", true, "")

	// 2. citations exist, belong to the tenant, and were in retrieved evidence
	evidenceIDs := map[string]bool{}
	for _, e := range c.Evidence {
		evidenceIDs[e.ArticleID] = true
	}
	citationsOK := true
	for _, id := range draft.CitedArticleIDs {
		if !evidenceIDs[id] {
			citationsOK = false
			push("citation_membership", false, id+" not in retrieved evidence")
		}
		if t, ok := c.ArticleTenant[id]; ok && t != c.TenantID {
			citationsOK = false
			push("citation_tenant", false, id+" belongs to another tenant")
		}
	}
	if citationsOK {
		push("citation_membership_and_tenant", true, "")
	}

	// 3. factual references point at real sources and values match
	sourceText := func(articleID string) (string, bool) {
		if articleID == "CRM_ORDER" {
			return c.OrderText, true
		}
		for _, e := range c.Evidence {
			if e.ArticleID == articleID {
				return e.Text, true
			}
		}
		return "", false
	}
	factsOK := true
	for _, ref := range draft.FactualReferences {
		src, ok := sourceText(ref.SourceArticleID)
		if !ok {
			factsOK = false
			push("factual_reference_source", false, ref.FactKey+" source missing")
			continue
		}
		if !strings.Contains(japanese.Normalize(src), japanese.Normalize(ref.NormalizedValue)) {
			factsOK = false
			push("factual_reference_value", false, ref.FactKey+"="+ref.NormalizedValue+" not in source")
		}
		if !strings.Contains(japanese.Normalize(draft.Body), japanese.Normalize(ref.NormalizedValue)) {
			factsOK = false
			push("factual_reference_in_body", false, ref.FactKey+"="+ref.NormalizedValue+" not in draft body")
		}
	}
	if factsOK {
		push("factual_references", true, "")
	}

	// 4. policy-sensitive values in the body must be supported by evidence/order
	supported := map[string]bool{}
	for _, e := range c.Evidence {
		for _, v := range japanese.ExtractValues(e.Text) {
			supported[v] = true
		}
	}
	if c.Order != nil {
		supported[c.Order.DeliveryStatus] = true
		supported[c.Order.Item] = true
	}
	unsupported := []string{}
	for _, v := range japanese.ExtractValues(draft.Body) {
		if !supported[v] {
			unsupported = append(unsupported, v)
		}
	}
	push("policy_sensitive_values", len(unsupported) == 0, strings.Join(unsupported, ","))

	// 5+6. signature, register, banned phrases
	style := japanese.CheckStyle(draft.Body)
	detail := ""
	if !style.OK {
		detail = strings.Join(style.Violations, ",")
	}
	push("japanese_style", style.OK, detail)
	return result()
}
