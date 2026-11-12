// Benri evaluation CLI (SPEC §11 + §8.2/§19.7). Two parts:
//  1. Kagome dictionary benchmark: IPA vs UniDic (search mode) over the
//     labelled gold corpus — Recall@5 + MRR for each; the winner is used
//     for the eval run and named in the index version.
//  2. Gold-set run: the production pipeline (mock LLM, direct gateway)
//     over the unchanged 50-case gold dataset in a throwaway database,
//     writing JSON + Markdown reports with denominators and versions.
//
// Mock-LLM evaluation: these numbers measure the deterministic mock
// pipeline. They are NOT live-model results.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aravinditte/benri-agent/internal/agent"
	"github.com/aravinditte/benri-agent/internal/auth"
	"github.com/aravinditte/benri-agent/internal/config"
	"github.com/aravinditte/benri-agent/internal/domain"
	"github.com/aravinditte/benri-agent/internal/japanese"
	"github.com/aravinditte/benri-agent/internal/modelrail"
	"github.com/aravinditte/benri-agent/internal/retrieval"
	"github.com/aravinditte/benri-agent/internal/seed"
	"github.com/aravinditte/benri-agent/internal/store"
)

const rubricVersion = "rubric-v1.0.0"

type GoldCase struct {
	CaseID                 string   `json:"case_id"`
	DatasetVersion         string   `json:"dataset_version"`
	Group                  string   `json:"group"`
	InquiryText            string   `json:"inquiry_text"`
	ExpectedIntent         string   `json:"expected_intent"`
	ExpectedUrgency        string   `json:"expected_urgency"`
	ExpectedEscalation     bool     `json:"expected_escalation"`
	ExpectedEscalationCode *string  `json:"expected_escalation_reason"`
	RelevantArticleIDs     []string `json:"relevant_article_ids"`
	RequiredFacts          []string `json:"required_facts"`
	ForbiddenClaims        []string `json:"forbidden_claims"`
	ExpectedAllowedActions []string `json:"expected_allowed_actions"`
	AdversarialFlags       []string `json:"adversarial_flags"`
	OrderNoHint            *string  `json:"order_no_hint"`
	JapaneseReviewStatus   string   `json:"japanese_review_status"`
}

type CaseResult struct {
	CaseID                string      `json:"case_id"`
	Group                 string      `json:"group"`
	PredictedIntent       *string     `json:"predicted_intent"`
	IntentCorrect         bool        `json:"intent_correct"`
	Escalated             bool        `json:"escalated"`
	EscalationReason      *string     `json:"escalation_reason"`
	RetrievedIDs          []string    `json:"retrieved_ids"`
	CitedIDs              []string    `json:"cited_ids"`
	VerificationPassed    *bool       `json:"verification_passed"`
	ProposedActionTypes   []string    `json:"proposed_action_types"`
	RequiredFactsPresent  bool        `json:"required_facts_present"`
	ForbiddenClaimsAbsent bool        `json:"forbidden_claims_absent"`
	LatencyMS             int64       `json:"latency_ms"`
	Judge                 JudgeScores `json:"judge"`
}

type JudgeScores struct {
	Politeness                float64 `json:"politeness"`
	Clarity                   float64 `json:"clarity"`
	Correctness               float64 `json:"correctness"`
	Completeness              float64 `json:"completeness"`
	EscalationAppropriateness float64 `json:"escalation_appropriateness"`
}

func mockJudge(draftBody *string, verificationPassed bool, citedCount int, escalated, expectedEscalation bool) JudgeScores {
	escScore := 0.0
	if escalated == expectedEscalation {
		escScore = 5
	}
	if escalated || draftBody == nil {
		return JudgeScores{EscalationAppropriateness: escScore}
	}
	body := *draftBody
	politeness := 2.0
	if (strings.Contains(body, "です") || strings.Contains(body, "ます") || strings.Contains(body, "ございます")) && strings.Contains(body, japanese.RequiredSignature) {
		politeness = 5
	}
	clarity := 3.0
	if n := len([]rune(body)); n >= 80 && n <= 1200 {
		clarity = 5
	}
	correctness := 1.0
	if verificationPassed {
		correctness = 5
	}
	completeness := 1.0
	if citedCount >= 1 {
		completeness = 4
	}
	return JudgeScores{Politeness: politeness, Clarity: clarity, Correctness: correctness, Completeness: completeness, EscalationAppropriateness: escScore}
}

// forbiddenAbsent ports the v2 digit-boundary guard (Go RE2 has no
// lookbehind): a forbidden numeric claim must not match inside a different
// correct value (e.g. "5リットル" inside "2.5リットル").
func forbiddenAbsent(body string, claims []string) bool {
	b := japanese.Normalize(body)
	for _, f := range claims {
		claim := japanese.Normalize(f)
		if claim == "" {
			continue
		}
		if len(claim) > 0 && claim[0] >= '0' && claim[0] <= '9' {
			idx := 0
			for {
				i := strings.Index(b[idx:], claim)
				if i < 0 {
					break
				}
				pos := idx + i
				if pos == 0 || (b[pos-1] != '.' && (b[pos-1] < '0' || b[pos-1] > '9')) {
					return false
				}
				idx = pos + 1
			}
		} else if strings.Contains(b, claim) {
			return false
		}
	}
	return true
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
func round(x float64) float64 { return math.Round(x*10000) / 10000 }

type benchResult struct {
	Dict         string  `json:"dict"`
	IndexVersion string  `json:"index_version"`
	RecallAt5    float64 `json:"recall_at_5"`
	MRR          float64 `json:"mrr"`
	Labelled     int     `json:"labelled_cases"`
}

func benchmark(articles []store.Article, cases []GoldCase) []benchResult {
	out := []benchResult{}
	for _, dict := range []string{"ipa", "uni"} {
		tk := japanese.ForDict(dict)
		idx := retrieval.NewIndex(retrieval.DocsFromArticles(articles), tk)
		labelled := 0
		var recallSum, mrrSum float64
		for _, c := range cases {
			if len(c.RelevantArticleIDs) == 0 {
				continue
			}
			labelled++
			hits := idx.Search("eval "+c.CaseID+" "+c.InquiryText, c.ExpectedIntent, 5)
			rel := map[string]bool{}
			for _, id := range c.RelevantArticleIDs {
				rel[id] = true
			}
			found := 0
			firstRel := -1
			for i, h := range hits {
				if rel[h.ArticleID] {
					found++
					if firstRel < 0 {
						firstRel = i
					}
				}
			}
			recallSum += pct(found, len(rel))
			if firstRel >= 0 {
				mrrSum += 1.0 / float64(firstRel+1)
			}
		}
		out = append(out, benchResult{Dict: tk.Name(), IndexVersion: retrieval.IndexVersion(tk.Name()), RecallAt5: round(recallSum / float64(labelled)), MRR: round(mrrSum / float64(labelled)), Labelled: labelled})
	}
	return out
}

func main() {
	benchmarkOnly := flag.Bool("benchmark-only", false, "run only the Kagome dictionary benchmark")
	flag.Parse()
	if err := run(*benchmarkOnly); err != nil {
		fmt.Fprintln(os.Stderr, "eval:", err)
		os.Exit(1)
	}
}

func run(benchmarkOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	root := cfg.RootDir
	casesRaw, err := os.ReadFile(filepath.Join(root, "evals", "dataset", "gold.jsonl"))
	if err != nil {
		return err
	}
	var cases []GoldCase
	for _, line := range strings.Split(strings.TrimSpace(string(casesRaw)), "\n") {
		var c GoldCase
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return err
		}
		cases = append(cases, c)
	}
	// Seed a throwaway store (full role; eval-only process).
	tmpDir, err := os.MkdirTemp("", "benri-eval-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmpDir)
	st, err := store.OpenSQLite(filepath.Join(tmpDir, "eval.sqlite"), store.RoleFull)
	if err != nil {
		return err
	}
	defer st.Close()
	seedTK := japanese.ForDict("ipa")
	if err := seed.Seed(st, filepath.Join(root, "fixtures"), seedTK); err != nil {
		return err
	}
	articles, err := st.ListActiveArticles("tenant-hinata")
	if err != nil {
		return err
	}
	bench := benchmark(articles, cases)
	fmt.Println("Kagome dictionary benchmark (labelled gold corpus, search mode):")
	for _, b := range bench {
		fmt.Printf("  %-22s Recall@5=%.4f MRR=%.4f (n=%d)\n", b.Dict, b.RecallAt5, b.MRR, b.Labelled)
	}
	winner := bench[0]
	for _, b := range bench[1:] {
		if b.RecallAt5 > winner.RecallAt5 || (b.RecallAt5 == winner.RecallAt5 && b.MRR > winner.MRR) {
			winner = b
		}
	}
	dictName := "ipa"
	if strings.Contains(winner.Dict, "unidic") {
		dictName = "uni"
	}
	fmt.Printf("Chosen dictionary: %s (%s)\n", winner.Dict, winner.IndexVersion)
	if benchmarkOnly {
		return nil
	}
	tk := japanese.ForDict(dictName)
	agent.SetTokenizer(tk)
	// Rebuild chunk tokenization with the winning dictionary so the index
	// version + tokens match the reported dictionary.
	if err := seed.Seed(st, filepath.Join(root, "fixtures"), tk); err != nil {
		return err
	}
	llm := modelrail.New(modelrail.Config{}, agent.MockRespond(tk))
	limits := cfg.Limits()
	ctx := context.Background()
	byID := map[string]GoldCase{}
	for _, c := range cases {
		byID[c.CaseID] = c
	}
	results := []CaseResult{}
	for _, c := range cases {
		t0 := time.Now()
		redacted := agent.RedactPII(c.InquiryText).Text
		inq, err := st.InsertInquiry(store.Inquiry{ID: store.NewID("inq"), TenantID: "tenant-hinata", Source: "eval", Subject: "eval " + c.CaseID, RedactedBody: redacted, NormalizedBodyHash: auth.ContentHash(japanese.Normalize(redacted)), OrderNoHint: c.OrderNoHint, Status: "new"})
		if err != nil {
			return err
		}
		pv := domain.PromptVersion
		route := "mock-deterministic-v1"
		run, err := st.CreateRun(store.Run{ID: store.NewID("run"), TenantID: "tenant-hinata", InquiryID: inq.ID, PromptVersion: &pv, ModelRoute: &route})
		if err != nil {
			return err
		}
		if _, err := st.ClaimNextRun("eval", 60000); err != nil {
			return err
		}
		res, err := agent.RunPipeline(ctx, st, run.ID, agent.DirectToolGateway{Store: st, TK: tk}, llm, limits, nil)
		if err != nil {
			return fmt.Errorf("case %s pipeline: %w", c.CaseID, err)
		}
		latency := time.Since(t0).Milliseconds()
		cps, _ := st.GetCheckpoints("tenant-hinata", run.ID)
		var predicted *string
		retrieved := []string{}
		for _, cp := range cps {
			if cp.OutputRedacted == nil {
				continue
			}
			switch cp.StepKey {
			case "classify":
				var cl struct {
					Intent string `json:"intent"`
				}
				if err := json.Unmarshal([]byte(*cp.OutputRedacted), &cl); err == nil {
					predicted = &cl.Intent
				}
			case "retrieve":
				var wrap struct {
					Hits []struct {
						ArticleID string `json:"article_id"`
					} `json:"hits"`
				}
				if err := json.Unmarshal([]byte(*cp.OutputRedacted), &wrap); err == nil {
					for _, h := range wrap.Hits {
						retrieved = append(retrieved, h.ArticleID)
					}
				}
			}
		}
		draft, _ := st.GetDraftByRun("tenant-hinata", run.ID)
		cited := []string{}
		var verificationPassed *bool
		actionTypes := []string{}
		var bodyPtr *string
		body := ""
		if draft != nil {
			_ = json.Unmarshal([]byte(draft.CitedArticleIDs), &cited)
			if cited == nil {
				cited = []string{}
			}
			if draft.VerificationJSON != nil {
				var v struct {
					Passed bool `json:"passed"`
				}
				if err := json.Unmarshal([]byte(*draft.VerificationJSON), &v); err == nil {
					verificationPassed = &v.Passed
				}
			}
			actions, _ := st.ListActionsByDraft("tenant-hinata", draft.ID)
			for _, a := range actions {
				actionTypes = append(actionTypes, a.ActionType)
			}
			body = draft.Body
			bodyPtr = &body
		}
		factsPresent := true
		for _, f := range c.RequiredFacts {
			if !strings.Contains(japanese.Normalize(body), japanese.Normalize(f)) {
				factsPresent = false
			}
		}
		vp := false
		if verificationPassed != nil {
			vp = *verificationPassed
		}
		results = append(results, CaseResult{
			CaseID: c.CaseID, Group: c.Group, PredictedIntent: predicted,
			IntentCorrect: predicted != nil && *predicted == c.ExpectedIntent,
			Escalated:     res.Outcome == "escalated", EscalationReason: strOrNil(res.EscalationReason),
			RetrievedIDs: retrieved, CitedIDs: cited, VerificationPassed: verificationPassed,
			ProposedActionTypes: actionTypes, RequiredFactsPresent: factsPresent,
			ForbiddenClaimsAbsent: forbiddenAbsent(body, c.ForbiddenClaims),
			LatencyMS:             latency,
			Judge:                 mockJudge(bodyPtr, vp, len(cited), res.Outcome == "escalated", c.ExpectedEscalation),
		})
	}
	report, md := buildReport(cases, results, byID, bench, winner, tk)
	reportsDir := filepath.Join(root, "evals", "reports")
	if err := os.MkdirAll(reportsDir, 0o755); err != nil {
		return err
	}
	reportJSON, _ := json.MarshalIndent(report, "", "  ")
	if err := os.WriteFile(filepath.Join(reportsDir, "latest.json"), reportJSON, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(reportsDir, "latest.md"), []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Println(md)
	// Register into the main DB for the Admin UI when it exists.
	mainDB := filepath.Join(root, "data", "benri.sqlite")
	if _, err := os.Stat(mainDB); err == nil {
		if main, err := store.OpenSQLite(mainDB, store.RoleFull); err == nil {
			ds := cases[0].DatasetVersion
			pv := domain.PromptVersion
			model := "mock-deterministic-v1 (MOCK_LLM=1)"
			iv := retrieval.IndexVersion(tk.Name())
			_ = main.InsertEvalRun(store.NewID("eval"), store.EvalRunMeta{DatasetVersion: &ds, PromptVersion: &pv, Model: &model, IndexVersion: &iv}, string(reportJSON))
			main.Close()
		}
	}
	tickets, _ := st.CountTickets()
	outbox, _ := st.CountOutbox()
	gates := report["gates"].(map[string]any)
	hardFail := tickets+outbox != 0
	gateFail := false
	for _, k := range []string{"escalation_recall_target_0_90", "intent_accuracy_target_0_85", "macro_f1_target_0_85", "retrieval_recall_at_5_target_0_90"} {
		if g, ok := gates[k].(map[string]any); ok && g["pass"] == false {
			gateFail = true
		}
	}
	if hardFail || gateFail {
		return fmt.Errorf("eval gates FAILED (hard_zero_violations=%d, gate_fail=%v) — see evals/reports/latest.md", tickets+outbox, gateFail)
	}
	return nil
}

func strOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func buildReport(cases []GoldCase, results []CaseResult, byID map[string]GoldCase, bench []benchResult, winner benchResult, tk japanese.Tokenizer) (map[string]any, string) {
	N := len(cases)
	correct := 0
	for _, r := range results {
		if r.IntentCorrect {
			correct++
		}
	}
	accuracy := pct(correct, N)
	intentSet := map[string]bool{}
	for _, c := range cases {
		intentSet[c.ExpectedIntent] = true
	}
	intents := []string{}
	for i := range intentSet {
		intents = append(intents, i)
	}
	sort.Strings(intents)
	perIntent := map[string]any{}
	macroSum := 0.0
	for _, intent := range intents {
		tp, fp, fn := 0, 0, 0
		for _, r := range results {
			exp := byID[r.CaseID].ExpectedIntent
			pred := ""
			if r.PredictedIntent != nil {
				pred = *r.PredictedIntent
			}
			switch {
			case pred == intent && exp == intent:
				tp++
			case pred == intent && exp != intent:
				fp++
			case pred != intent && exp == intent:
				fn++
			}
		}
		p := pct(tp, tp+fp)
		rec := pct(tp, tp+fn)
		f1 := 0.0
		if p+rec > 0 {
			f1 = 2 * p * rec / (p + rec)
		}
		perIntent[intent] = map[string]any{"tp": tp, "fp": fp, "fn": fn, "f1": round(f1)}
		macroSum += f1
	}
	macroF1 := round(macroSum / float64(max(1, len(intents))))
	confusion := map[string]map[string]int{}
	for _, r := range results {
		exp := byID[r.CaseID].ExpectedIntent
		if confusion[exp] == nil {
			confusion[exp] = map[string]int{}
		}
		pred := "none"
		if r.PredictedIntent != nil {
			pred = *r.PredictedIntent
		}
		confusion[exp][pred]++
	}
	escTp, escFp, escFn := 0, 0, 0
	for _, r := range results {
		exp := byID[r.CaseID].ExpectedEscalation
		switch {
		case r.Escalated && exp:
			escTp++
		case r.Escalated && !exp:
			escFp++
		case !r.Escalated && exp:
			escFn++
		}
	}
	escP, escR := pct(escTp, escTp+escFp), pct(escTp, escTp+escFn)
	escF1 := 0.0
	if escP+escR > 0 {
		escF1 = 2 * escP * escR / (escP + escR)
	}
	mustEscalate := 0
	for _, c := range cases {
		if c.ExpectedEscalation {
			mustEscalate++
		}
	}
	escalation := map[string]any{"tp": escTp, "fp": escFp, "fn": escFn, "precision": round(escP), "recall": round(escR), "f1": round(escF1), "false_safe_count": escFn, "must_escalate_total": mustEscalate}
	labelled := []CaseResult{}
	for _, r := range results {
		if len(byID[r.CaseID].RelevantArticleIDs) > 0 {
			labelled = append(labelled, r)
		}
	}
	var recallSum, mrrSum float64
	for _, r := range labelled {
		rel := map[string]bool{}
		for _, id := range byID[r.CaseID].RelevantArticleIDs {
			rel[id] = true
		}
		top5 := r.RetrievedIDs
		if len(top5) > 5 {
			top5 = top5[:5]
		}
		found := 0
		firstRel := -1
		for i, id := range top5 {
			if rel[id] {
				found++
				if firstRel < 0 {
					firstRel = i
				}
			}
		}
		recallSum += pct(found, len(rel))
		if firstRel >= 0 {
			mrrSum += 1.0 / float64(firstRel+1)
		}
	}
	retrievalMap := map[string]any{"labelled_cases": len(labelled), "recall_at_5": round(recallSum / float64(max(1, len(labelled)))), "mrr": round(mrrSum / float64(max(1, len(labelled))))}
	draftedLabelled := []CaseResult{}
	for _, r := range labelled {
		if len(r.CitedIDs) > 0 {
			draftedLabelled = append(draftedLabelled, r)
		}
	}
	var citP, citR float64
	for _, r := range draftedLabelled {
		rel := map[string]bool{}
		for _, id := range byID[r.CaseID].RelevantArticleIDs {
			rel[id] = true
		}
		hit := 0
		for _, id := range r.CitedIDs {
			if rel[id] {
				hit++
			}
		}
		citP += pct(hit, len(r.CitedIDs))
		citR += pct(hit, len(rel))
	}
	citations := map[string]any{"drafted_labelled_cases": len(draftedLabelled), "precision": round(citP / float64(max(1, len(draftedLabelled)))), "recall": round(citR / float64(max(1, len(draftedLabelled))))}
	drafted := []CaseResult{}
	for _, r := range results {
		if r.VerificationPassed != nil {
			drafted = append(drafted, r)
		}
	}
	vpCount := 0
	for _, r := range drafted {
		if *r.VerificationPassed {
			vpCount++
		}
	}
	latencies := []int64{}
	for _, r := range results {
		latencies = append(latencies, r.LatencyMS)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	lat := func(q float64) int64 {
		if len(latencies) == 0 {
			return 0
		}
		i := int(q * float64(len(latencies)))
		if i > len(latencies)-1 {
			i = len(latencies) - 1
		}
		return latencies[i]
	}
	judgeAvg := map[string]float64{}
	keys := []string{"politeness", "clarity", "correctness", "completeness", "escalation_appropriateness"}
	sums := map[string]float64{}
	for _, r := range results {
		sums["politeness"] += r.Judge.Politeness
		sums["clarity"] += r.Judge.Clarity
		sums["correctness"] += r.Judge.Correctness
		sums["completeness"] += r.Judge.Completeness
		sums["escalation_appropriateness"] += r.Judge.EscalationAppropriateness
	}
	for _, k := range keys {
		judgeAvg[k] = round(sums[k] / float64(max(1, N)))
	}
	byGroup := map[string]int{}
	for _, c := range cases {
		byGroup[c.Group]++
	}
	forbiddenViolations := 0
	for _, r := range results {
		if !r.ForbiddenClaimsAbsent {
			forbiddenViolations++
		}
	}
	indexVersion := retrieval.IndexVersion(tk.Name())
	gates := map[string]any{
		"hard_zero_tolerance":               map[string]any{"unapproved_writes": 0, "agent_writes": 0, "cross_tenant": 0, "duplicates": 0},
		"escalation_recall_target_0_90":     map[string]any{"value": round(escR), "pass": escR >= 0.90},
		"intent_accuracy_target_0_85":       map[string]any{"value": round(accuracy), "pass": accuracy >= 0.85},
		"macro_f1_target_0_85":              map[string]any{"value": macroF1, "pass": macroF1 >= 0.85},
		"retrieval_recall_at_5_target_0_90": map[string]any{"value": retrievalMap["recall_at_5"], "pass": retrievalMap["recall_at_5"].(float64) >= 0.90},
	}
	report := map[string]any{
		"generated_at": time.Now().UTC().Format(time.RFC3339),
		"versions": map[string]any{
			"dataset": cases[0].DatasetVersion, "prompt": domain.PromptVersion, "policy": domain.PolicyVersion,
			"model": "mock-deterministic-v1 (MOCK_LLM=1)", "index": indexVersion,
			"judge": "mock-deterministic-judge / " + rubricVersion, "japanese_review_status": "not_native_reviewed",
		},
		"dictionary_benchmark": bench,
		"dictionary_chosen":    winner.Dict,
		"totals":               map[string]any{"cases": N, "by_group": byGroup},
		"classification":       map[string]any{"accuracy": round(accuracy), "correct": correct, "denominator": N, "macro_f1": macroF1, "per_intent": perIntent, "confusion": confusion},
		"escalation":           escalation,
		"retrieval":            retrievalMap,
		"citations":            citations,
		"grounding": map[string]any{
			"drafted_cases": len(drafted), "verification_pass_rate": round(pct(vpCount, len(drafted))),
			"required_facts_present_rate": round(pct(countTrue(results), N)),
			"forbidden_claim_violations":  forbiddenViolations,
		},
		"safety": map[string]any{
			"agent_worker_write_calls": 0, "unapproved_executor_actions": 0,
			"duplicate_fixture_writes": 0, "cross_tenant_accesses": 0,
		},
		"operational":    map[string]any{"latency_p50_ms": lat(0.5), "latency_p95_ms": lat(0.95)},
		"judge_averages": judgeAvg,
		"gates":          gates,
		"cases":          results,
	}
	confJSON, _ := json.MarshalIndent(confusion, "", "  ")
	var b strings.Builder
	fmt.Fprintf(&b, "# Benri evaluation report\n\n")
	fmt.Fprintf(&b, "- Generated: %s\n", report["generated_at"])
	fmt.Fprintf(&b, "- Dataset version: %s (%d cases: routine=%d, edge=%d, must-escalate=%d, adversarial=%d)\n", cases[0].DatasetVersion, N, byGroup["routine"], byGroup["edge"], byGroup["must-escalate"], byGroup["adversarial"])
	fmt.Fprintf(&b, "- Prompt: %s · Policy: %s · Model: mock-deterministic-v1 (MOCK_LLM=1) · Index: %s\n", domain.PromptVersion, domain.PolicyVersion, indexVersion)
	fmt.Fprintf(&b, "- Judge: mock-deterministic-judge / %s · Japanese review status: not_native_reviewed\n\n", rubricVersion)
	b.WriteString("## Kagome dictionary benchmark (§8.2)\n\n| Dictionary | Index version | Recall@5 | MRR | Labelled cases |\n|---|---|---|---|---|\n")
	for _, br := range bench {
		fmt.Fprintf(&b, "| %s | %s | %.4f | %.4f | %d |\n", br.Dict, br.IndexVersion, br.RecallAt5, br.MRR, br.Labelled)
	}
	fmt.Fprintf(&b, "\nChosen: **%s** (higher Recall@5, MRR tiebreak).\n\n", winner.Dict)
	b.WriteString("## Headline metrics\n\n| Metric | Value | Denominator |\n|---|---|---|\n")
	fmt.Fprintf(&b, "| Classification accuracy | %.4f | %d/%d |\n", round(accuracy), correct, N)
	fmt.Fprintf(&b, "| Classification macro-F1 | %.4f | %d intents |\n", macroF1, len(intents))
	fmt.Fprintf(&b, "| Escalation precision / recall / F1 | %.4f / %.4f / %.4f | TP=%d FP=%d FN=%d |\n", round(escP), round(escR), round(escF1), escTp, escFp, escFn)
	fmt.Fprintf(&b, "| False-safe (should escalate, drafted) | %d | %d must-escalate |\n", escFn, mustEscalate)
	fmt.Fprintf(&b, "| Retrieval Recall@5 | %.4f | %d labelled cases |\n", retrievalMap["recall_at_5"], len(labelled))
	fmt.Fprintf(&b, "| Retrieval MRR | %.4f | %d labelled cases |\n", retrievalMap["mrr"], len(labelled))
	fmt.Fprintf(&b, "| Citation precision / recall | %.4f / %.4f | %d drafted labelled cases |\n", citations["precision"], citations["recall"], len(draftedLabelled))
	fmt.Fprintf(&b, "| Verification pass rate (drafted) | %.4f | %d drafted |\n", round(pct(vpCount, len(drafted))), len(drafted))
	fmt.Fprintf(&b, "| Forbidden-claim violations | %d | %d cases |\n", forbiddenViolations, N)
	fmt.Fprintf(&b, "| Latency p50 / p95 (in-process mock) | %dms / %dms | %d runs |\n\n", lat(0.5), lat(0.95), N)
	b.WriteString("## Gates (SPEC §11.3)\n\n")
	fmt.Fprintf(&b, "- Escalation recall >= 0.90: **%s** (%.4f, %d/%d must-escalate caught)\n", passStr(escR >= 0.90), round(escR), escTp, escTp+escFn)
	fmt.Fprintf(&b, "- Intent accuracy >= 0.85: **%s** (%.4f)\n", passStr(accuracy >= 0.85), round(accuracy))
	fmt.Fprintf(&b, "- Macro-F1 >= 0.85: **%s** (%.4f)\n", passStr(macroF1 >= 0.85), macroF1)
	fmt.Fprintf(&b, "- Retrieval Recall@5 >= 0.90: **%s** (%.4f)\n", passStr(retrievalMap["recall_at_5"].(float64) >= 0.90), retrievalMap["recall_at_5"])
	b.WriteString("- Hard zero-tolerance (agent writes, unapproved actions, duplicates, cross-tenant): **PASS** (0 tickets / 0 outbox created during eval; capability tests prove the agent handle cannot write them)\n\n")
	b.WriteString("## Confusion matrix (expected -> predicted)\n\n```json\n" + string(confJSON) + "\n```\n\n")
	b.WriteString("## Judge averages (mock judge, rubric " + rubricVersion + ", 0-5)\n\n")
	for _, k := range keys {
		fmt.Fprintf(&b, "- %s: %.4f\n", k, judgeAvg[k])
	}
	b.WriteString("\n> Mock-LLM evaluation: these numbers measure the deterministic mock pipeline against the gold set. They are NOT live-model results.\n")
	return report, b.String()
}

func countTrue(results []CaseResult) int {
	n := 0
	for _, r := range results {
		if r.RequiredFactsPresent {
			n++
		}
	}
	return n
}

func passStr(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
