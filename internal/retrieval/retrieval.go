// Package retrieval implements Japanese lexical retrieval (SPEC §8.2).
// This is LEXICAL retrieval — NOT BM25. Tokenization uses Kagome v2 in
// Search segmentation mode (dictionary selected by benchmark, §19.7) over
// NFKC-normalized text. Scoring is the v2 approach ported: IDF-weighted
// content coverage with an intent-aware evidence gate; thresholds were
// calibrated on the labelled gold set (SPEC §8.3) and are recalibrated
// against Kagome tokens in the v3 eval.
package retrieval

import (
	"math"
	"sort"
	"strings"

	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/store"
)

// IndexVersion names the tokenizer dictionary + version (§8.2: dictionary
// changes alter the index, so it is part of the version string).
func IndexVersion(dictName string) string {
	if strings.Contains(dictName, "unidic") || dictName == "uni" {
		return "lexical-v3-kagome-unidic-1.2.6"
	}
	return "lexical-v3-kagome-ipa-1.2.6"
}

const (
	ContentCoverageThreshold = 0.18
	IntentGateThreshold      = 0.12
)

var queryStopPhrases = []string{"について教えてください", "を教えてください", "について", "おしえてください", "教えてください", "おねがいします", "お願いします", "いただけますか", "いただけますでしょうか", "くださいますか", "でしょうか", "ですか", "ますか"}

var queryExpansions = map[string]string{"へんぴん": "返品", "ざいこ": "在庫", "とうちゃく": "到着", "りょうしゅうしょ": "領収書"}

var orderNoRe = strings.NewReplacer("[注文番号]", " ", "注文番号", " ")

func stripQueryScaffolding(query string, tk japanese.Tokenizer) string {
	q := japanese.Normalize(query)
	for _, p := range queryStopPhrases {
		q = strings.ReplaceAll(q, p, " ")
	}
	// Order-number mentions are identifiers, not topical evidence.
	q = orderNoRe.Replace(q)
	// Remove raw order numbers like HIN-2026-0001 (ascii after NFKC).
	var b strings.Builder
	for _, f := range strings.FieldsFunc(q, func(r rune) bool { return r == ' ' || r == '　' }) {
		if len(f) >= 8 && strings.Contains(f, "-") {
			continue
		}
		b.WriteString(f)
		b.WriteString(" ")
	}
	q = b.String()
	for from, to := range queryExpansions {
		if strings.Contains(q, from) {
			q += " " + to
		}
	}
	return q
}

// Tokenize is the shared tokenization used by the index, the query path and
// the mock draft builder: Kagome Search-mode surfaces plus latin/digit word
// tokens, lowercased after NFKC (as in v2).
func Tokenize(text string, tk japanese.Tokenizer) []string {
	norm := strings.ToLower(japanese.Normalize(text))
	toks := tk.Tokenize(norm)
	// Kagome splits latin/digit runs character-wise in some cases; also add
	// whole word tokens for ascii runs (v2 behaviour).
	for _, w := range strings.FieldsFunc(norm, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '/')
	}) {
		if len(w) >= 2 {
			toks = append(toks, w)
		}
	}
	return toks
}

func TokenizedText(text string, tk japanese.Tokenizer) string {
	return strings.Join(Tokenize(text, tk), " ")
}

type IndexedDoc struct {
	ArticleID  string
	ChunkID    string
	Title      string
	Text       string
	IntentTags []string
}

type SearchHit struct {
	ArticleID    string  `json:"article_id"`
	ChunkID      string  `json:"chunk_id"`
	Title        string  `json:"title"`
	Excerpt      string  `json:"excerpt"`
	LexicalScore float64 `json:"lexical_score"`
}

type Index struct {
	docs      []IndexedDoc
	docTokens map[string]map[string]int
	df        map[string]int
	tk        japanese.Tokenizer
}

func NewIndex(docs []IndexedDoc, tk japanese.Tokenizer) *Index {
	idx := &Index{docTokens: map[string]map[string]int{}, df: map[string]int{}, tk: tk}
	idx.docs = docs
	for _, d := range docs {
		counts := map[string]int{}
		for _, t := range Tokenize(d.Title+" "+d.Text, tk) {
			counts[t]++
		}
		idx.docTokens[d.ChunkID] = counts
		for t := range counts {
			idx.df[t]++
		}
	}
	return idx
}

func DocsFromArticles(articles []store.Article) []IndexedDoc {
	docs := make([]IndexedDoc, 0, len(articles))
	for _, a := range articles {
		docs = append(docs, IndexedDoc{ArticleID: a.ID, ChunkID: a.ID + "-c0", Title: a.Title, Text: a.Body, IntentTags: a.IntentTags})
	}
	return docs
}

func isContentToken(t string) bool {
	if len([]rune(t)) < 2 {
		return false
	}
	for _, r := range t {
		if (r >= '一' && r <= '鿿') || (r >= 'ァ' && r <= 'ヶ') {
			return true
		}
	}
	return false
}

// Search ranks documents by IDF-weighted anchor-token score gated on
// IDF-weighted content coverage (intent-matched docs get the lower gate).
func (idx *Index) Search(query string, intent string, limit int) []SearchHit {
	if limit <= 0 {
		limit = 5
	}
	cleaned := stripQueryScaffolding(query, idx.tk)
	qAll := Tokenize(cleaned, idx.tk)
	if len(qAll) == 0 {
		return nil
	}
	qSet := map[string]bool{}
	for _, t := range qAll {
		qSet[t] = true
	}
	var qContent []string
	for t := range qSet {
		if isContentToken(t) {
			qContent = append(qContent, t)
		}
	}
	n := float64(max(len(idx.docs), 1))
	idfOf := func(t string) float64 {
		dfv, ok := idx.df[t]
		if !ok {
			return 0.35 * math.Log(1+n)
		}
		return math.Log(1 + n/(1+float64(dfv)))
	}
	totalIdf := 0.0
	for _, t := range qContent {
		totalIdf += idfOf(t)
	}
	coverage := func(counts map[string]int) float64 {
		if totalIdf == 0 {
			return 1
		}
		covered := 0.0
		for _, t := range qContent {
			if _, ok := counts[t]; ok {
				covered += idfOf(t)
			}
		}
		return covered / totalIdf
	}
	hits := []SearchHit{}
	for _, d := range idx.docs {
		counts := idx.docTokens[d.ChunkID]
		anchor, uni := 0.0, 0.0
		for t := range qSet {
			tf, ok := counts[t]
			if !ok {
				continue
			}
			idf := math.Log(1 + n/(1+float64(idx.df[t])))
			if len([]rune(t)) >= 2 {
				anchor += idf * (1 + math.Log(float64(tf)))
			} else {
				uni += idf * (1 + math.Log(float64(tf)))
			}
		}
		score := anchor + 0.1*uni
		intentMatch := intent != "" && contains(d.IntentTags, intent)
		if intentMatch {
			score *= 1.15
		}
		gate := ContentCoverageThreshold
		if intentMatch {
			gate = IntentGateThreshold
		}
		if anchor > 0 && coverage(counts) >= gate {
			excerpt := []rune(d.Text)
			if len(excerpt) > 240 {
				excerpt = excerpt[:240]
			}
			hits = append(hits, SearchHit{ArticleID: d.ArticleID, ChunkID: d.ChunkID, Title: d.Title, Excerpt: string(excerpt), LexicalScore: round4(score)})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool { return hits[i].LexicalScore > hits[j].LexicalScore })
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
func round4(f float64) float64 { return math.Round(f*10000) / 10000 }

// ReciprocalRankFusion is documented for when dense retrieval is enabled.
// Currently unused for ranking because dense retrieval is disabled by the
// ModelRail embeddings capability spike (no embeddings endpoint found).
func ReciprocalRankFusion(rankings [][]string, k int) []struct {
	ID    string
	Score float64
} {
	if k <= 0 {
		k = 60
	}
	scores := map[string]float64{}
	for _, list := range rankings {
		for i, id := range list {
			scores[id] += 1.0 / float64(k+i+1)
		}
	}
	out := make([]struct {
		ID    string
		Score float64
	}, 0, len(scores))
	for id, sc := range scores {
		out = append(out, struct {
			ID    string
			Score float64
		}{id, sc})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}
