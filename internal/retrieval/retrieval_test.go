package retrieval_test

import (
	"strings"
	"testing"

	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/store"
	"github.com/aravinditte/benri-agent/internal/testutil"
)

func TestNFKCAndKagomeTokenization(t *testing.T) {
	tk := testutil.Tokenizer()
	toks := retrieval.Tokenize("返品は３０日以内です", tk)
	joined := strings.Join(toks, " ")
	if !strings.Contains(joined, "30日") && !strings.Contains(joined, "30") {
		t.Errorf("NFKC fullwidth digits not normalized in tokens: %v", toks)
	}
	jtoks := tk.Tokenize("配送状況を教えてください")
	found := false
	for _, tok := range jtoks {
		if tok == "配送" {
			found = true
		}
	}
	if !found {
		t.Errorf("Kagome must segment 配送状況 containing 配送: %v", jtoks)
	}
	if japanese.Normalize("３０日") != "30日" {
		t.Error("NFKC normalize failed")
	}
}

func TestRetrievalRankingAndNoHit(t *testing.T) {
	_, full := testutil.SeededDB(t)
	articles, err := full.ListActiveArticles("tenant-hinata")
	if err != nil {
		t.Fatal(err)
	}
	idx := retrieval.NewIndex(retrieval.DocsFromArticles(articles), testutil.Tokenizer())
	hits := idx.Search("配送状況を教えてください", "delivery_status", 5)
	if len(hits) == 0 {
		t.Fatal("delivery query returned no hits")
	}
	// Gold-labelled relevant articles for delivery-status queries are
	// KB-001/KB-002; KB-003 (delivery status confirmation) is also a
	// delivery article. The top hit must be one of them and KB-002 must
	// be inside the top 5.
	if hits[0].ArticleID != "KB-001" && hits[0].ArticleID != "KB-002" && hits[0].ArticleID != "KB-003" {
		t.Errorf("top hit for delivery query = %s, want a delivery article", hits[0].ArticleID)
	}
	foundKB002 := false
	for _, h := range hits {
		if h.ArticleID == "KB-002" {
			foundKB002 = true
		}
	}
	if !foundKB002 {
		t.Error("KB-002 (gold-labelled) must be in the top 5 for a delivery query")
	}
	returns := idx.Search("返品したいです。返品方法を教えてください", "return_exchange", 5)
	if len(returns) == 0 {
		t.Fatal("returns query returned no hits")
	}
	none := idx.Search("全然関係ない宇宙飛行士の話", "other", 5)
	if len(none) != 0 {
		t.Errorf("unrelated query must return no hits, got %d", len(none))
	}
	var _ = store.Article{}
}
