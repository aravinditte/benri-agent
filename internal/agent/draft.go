package agent

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/retrieval"
)

// Draft construction + deterministic MOCK LLM responder (SPEC §10.1:
// MOCK_LLM=1, case-keyed, clearly labelled), ported from v2. It implements
// the same structured contracts a live ModelRail model would: CLASSIFY
// returns JSON; DRAFT returns DraftOutput JSON with citations + factual
// references. It is NOT a live model result.

type EvidenceItem struct {
	ArticleID string `json:"article_id"`
	ChunkID   string `json:"chunk_id"`
	Title     string `json:"title"`
	Text      string `json:"text"`
}

type OrderContext struct {
	OrderNoMasked  string `json:"order_no_masked"`
	Item           string `json:"item"`
	DeliveryStatus string `json:"delivery_status"`
	OrderedAt      string `json:"ordered_at"`
}

type FactualReference struct {
	FactKey         string `json:"factKey"`
	NormalizedValue string `json:"normalizedValue"`
	SourceArticleID string `json:"sourceArticleId"`
	SourceChunkID   string `json:"sourceChunkId"`
}

type DraftOutput struct {
	Body              string             `json:"body"`
	CitedArticleIDs   []string           `json:"citedArticleIds"`
	FactualReferences []FactualReference `json:"factualReferences"`
}

// ExtractValues is japanese.ExtractValues (kept here for call-site parity).
func ExtractValues(text string) []string { return japanese.ExtractValues(text) }

// pickSentences mirrors v2: sentence scoring by Kagome token overlap with
// the query (v2 used bigram tokens; token source changed, algorithm same).
func pickSentences(text string, max int, query string, minOverlap int, tk japanese.Tokenizer) []string {
	parts := strings.SplitAfter(text, "。")
	sentences := []string{}
	for _, s := range parts {
		if strings.TrimSpace(s) != "" {
			sentences = append(sentences, strings.TrimSpace(s))
		}
	}
	if query == "" {
		if len(sentences) > max {
			return sentences[:max]
		}
		return sentences
	}
	qTokens := map[string]bool{}
	for _, t := range retrieval.Tokenize(query, tk) {
		if len([]rune(t)) >= 2 && containsCJK(t) {
			qTokens[t] = true
		}
	}
	type scored struct {
		s     string
		i     int
		score float64
	}
	var cands []scored
	for i, s := range sentences {
		sTokens := map[string]bool{}
		for _, t := range retrieval.Tokenize(s, tk) {
			sTokens[t] = true
		}
		overlap := 0
		for t := range qTokens {
			if sTokens[t] {
				overlap++
			}
		}
		score := float64(overlap)
		if len(ExtractValues(s)) > 0 {
			score += 0.5
		}
		if score >= float64(minOverlap)+0.5 || (minOverlap <= 1 && score > 0.5) {
			cands = append(cands, scored{s, i, score})
		}
	}
	sort.SliceStable(cands, func(a, b int) bool {
		if cands[a].score != cands[b].score {
			return cands[a].score > cands[b].score
		}
		return cands[a].i < cands[b].i
	})
	if len(cands) > max {
		cands = cands[:max]
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].i < cands[b].i })
	picked := make([]string, 0, len(cands))
	for _, c := range cands {
		picked = append(picked, c.s)
	}
	if len(picked) == 0 && minOverlap <= 1 && len(sentences) > 0 {
		return sentences[:1]
	}
	return picked
}

func containsCJK(s string) bool {
	for _, r := range s {
		if (r >= '一' && r <= '鿿') || (r >= 'ァ' && r <= 'ヶ') {
			return true
		}
	}
	return false
}

// asTokenizer adapts the japanese.Tokenizer interface (structural).
type tkAdapter struct {
	tk interface{ Tokenize(string) []string }
}

func asTokenizer(tk interface{ Tokenize(string) []string }) interface{ Tokenize(string) []string } {
	return tk
}

// BuildDraft builds the evidence-bounded draft exactly as the v2 mock did:
// greeting, optional order line with CRM factual references, up to two
// evidence articles' best sentences with policy-value references, closing,
// and the required signature.
func BuildDraft(intent string, evidence []EvidenceItem, order *OrderContext, query string, tk japanese.Tokenizer) DraftOutput {
	lines := []string{"お問い合わせありがとうございます。ひなたストア カスタマーサポートです。"}
	refs := []FactualReference{}
	cited := []string{}
	if order != nil {
		lines = append(lines, "ご注文の商品「"+order.Item+"」につきまして、現在の配送状況は「"+order.DeliveryStatus+"」となっております。")
		refs = append(refs,
			FactualReference{FactKey: "delivery_status", NormalizedValue: order.DeliveryStatus, SourceArticleID: "CRM_ORDER", SourceChunkID: order.OrderNoMasked},
			FactualReference{FactKey: "item", NormalizedValue: order.Item, SourceArticleID: "CRM_ORDER", SourceChunkID: order.OrderNoMasked})
	}
	for idx, ev := range evidence {
		if idx >= 2 {
			break
		}
		max, minOv := 2, 2
		if idx == 0 {
			max, minOv = 3, 1
		}
		sentences := pickSentences(ev.Text, max, query, minOv, tk)
		if len(sentences) == 0 {
			continue
		}
		cited = append(cited, ev.ArticleID)
		for _, sentence := range sentences {
			lines = append(lines, sentence)
			for _, v := range ExtractValues(sentence) {
				refs = append(refs, FactualReference{FactKey: "policy_value", NormalizedValue: v, SourceArticleID: ev.ArticleID, SourceChunkID: ev.ChunkID})
			}
		}
	}
	lines = append(lines, "ご不明な点がございましたら、お気軽にお問い合わせください。")
	lines = append(lines, japanese.RequiredSignature)
	return DraftOutput{Body: strings.Join(lines, "\n"), CitedArticleIDs: cited, FactualReferences: refs}
}

// MockRespond implements modelrail.MockResponder with the v2 task protocol.
func MockRespond(tk japanese.Tokenizer) modelrail.MockResponder {
	return func(messages []modelrail.ChatMessage) (string, error) {
		var system, user string
		for _, m := range messages {
			if m.Role == "system" {
				system = m.Content
			}
			if m.Role == "user" {
				user = m.Content
			}
		}
		switch {
		case strings.Contains(system, "TASK:CLASSIFY"):
			var payload struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal([]byte(user), &payload); err != nil {
				return "", err
			}
			out, _ := json.Marshal(ClassifyText(payload.Text))
			return string(out), nil
		case strings.Contains(system, "TASK:DRAFT"):
			var payload struct {
				Intent   string         `json:"intent"`
				Evidence []EvidenceItem `json:"evidence"`
				Order    *OrderContext  `json:"order"`
				Query    string         `json:"query"`
			}
			if err := json.Unmarshal([]byte(user), &payload); err != nil {
				return "", err
			}
			out, _ := json.Marshal(BuildDraft(payload.Intent, payload.Evidence, payload.Order, payload.Query, tk))
			return string(out), nil
		}
		return `{"error":"unknown task"}`, nil
	}
}
