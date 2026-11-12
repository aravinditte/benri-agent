// Package japanese holds the Kagome tokenizer adapter (SPEC §8.2) and the
// deterministic mechanical style checks (SPEC §8.6, ported from v2).
//
// Mechanical checks enforce format, required phrases, length, and a short
// list of obvious register violations. They do NOT prove natural business
// Japanese; fixture content is marked not_native_reviewed.
package japanese

import (
	"strings"
	"sync"

	"github.com/ikawaha/kagome-dict/ipa"
	"github.com/ikawaha/kagome-dict/uni"
	"github.com/ikawaha/kagome/v2/tokenizer"
	"golang.org/x/text/unicode/norm"
)

// NFKC-normalize (SPEC §8.2) — must run before PII pattern matching,
// tokenization and comparison.
func Normalize(s string) string { return norm.NFKC.String(s) }

// Tokenizer is the small replaceable tokenizer interface (SPEC §8.2).
type Tokenizer interface {
	Name() string // dictionary name + version, e.g. "kagome-ipa-1.2.6"
	Tokenize(text string) []string
}

type kagomeTokenizer struct {
	tk   *tokenizer.Tokenizer
	name string
}

func (k *kagomeTokenizer) Name() string { return k.name }

// Tokenize returns surface forms in Kagome Search segmentation mode,
// excluding BOS/EOS dummy tokens.
func (k *kagomeTokenizer) Tokenize(text string) []string {
	toks := k.tk.Analyze(text, tokenizer.Search)
	out := make([]string, 0, len(toks))
	for _, t := range toks {
		if t.Surface == "" || t.Class == tokenizer.DUMMY {
			continue
		}
		out = append(out, t.Surface)
	}
	return out
}

var (
	cache   = map[string]Tokenizer{}
	cacheMu sync.Mutex
)

// ForDict returns the tokenizer for "ipa" (IPAdic) or "uni" (UniDic).
func ForDict(dict string) Tokenizer {
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if t, ok := cache[dict]; ok {
		return t
	}
	var tk *tokenizer.Tokenizer
	var err error
	var name string
	switch dict {
	case "uni":
		tk, err = tokenizer.New(uni.Dict())
		name = "kagome-unidic-1.2.6"
	default:
		dict = "ipa"
		tk, err = tokenizer.New(ipa.Dict())
		name = "kagome-ipa-1.2.6"
	}
	if err != nil {
		panic(err)
	}
	t := &kagomeTokenizer{tk: tk, name: name}
	cache[dict] = t
	return t
}

// DictOf returns the dictionary key for an index version produced by
// retrieval.IndexVersion.
func DictOf(indexVersion string) string {
	if strings.Contains(indexVersion, "unidic") {
		return "uni"
	}
	return "ipa"
}

// ---- mechanical style checks (SPEC §8.6, ported from v2) ----

const RequiredSignature = "ひなたストア カスタマーサポート"

var BannedPhrases = []string{
	"だよ", "だね", "じゃん", "っす", "マジ", "ヤバい",
	"絶対に補償", "必ず返金します", "100%保証", "無料にします", "割引します",
	"キャンペーン価格", "今だけ",
}

type StyleResult struct {
	OK         bool     `json:"ok"`
	Violations []string `json:"violations"`
}

func CheckStyle(body string) StyleResult {
	violations := []string{}
	if !strings.Contains(body, RequiredSignature) {
		violations = append(violations, "MISSING_SIGNATURE")
	}
	polite := strings.Contains(body, "です") || strings.Contains(body, "ます") || strings.Contains(body, "ございます") || strings.Contains(body, "いたします")
	if !polite {
		violations = append(violations, "NO_POLITE_REGISTER_MARKERS")
	}
	for _, p := range BannedPhrases {
		if strings.Contains(body, p) {
			violations = append(violations, "BANNED_PHRASE:"+p)
		}
	}
	for _, suf := range []string{"だ。", "だ！", "だよね", "なのだ。"} {
		if strings.Contains(body, suf) {
			violations = append(violations, "CASUAL_ENDING")
			break
		}
	}
	if len([]rune(body)) < 40 {
		violations = append(violations, "TOO_SHORT")
	}
	if len([]rune(body)) > 2000 {
		violations = append(violations, "TOO_LONG")
	}
	return StyleResult{OK: len(violations) == 0, Violations: violations}
}
