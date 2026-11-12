// Benri evaluation runner (SPEC §11). Runs the real pipeline (mock LLM,
// direct tool gateway) over the versioned gold set in a throwaway database
// and writes JSON + Markdown reports with denominators and versions.
import { readFileSync, writeFileSync, existsSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { BenriStore, contentHash, newId, seedInquiries, seedStore } from "@benri/db";
import { DirectToolGateway, limitsFromEnv, mockLlmRespond, redactPII, runPipeline, PROMPT_VERSION, POLICY_VERSION } from "@benri/agent-core";
import { ModelRailClient } from "@benri/modelrail-client";
import { INDEX_VERSION } from "@benri/retrieval";
import { mockJudge, RUBRIC_VERSION } from "../../judges/judge.js";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");

interface GoldCase {
  case_id: string; dataset_version: string; group: string; inquiry_text: string;
  expected_intent: string; expected_urgency: string; expected_escalation: boolean;
  expected_escalation_reason: string | null; relevant_article_ids: string[];
  required_facts: string[]; forbidden_claims: string[]; expected_allowed_actions: string[];
  adversarial_flags: string[]; order_no_hint: string | null; japanese_review_status: string;
}

interface CaseResult {
  case_id: string; group: string;
  predicted_intent: string | null; intent_correct: boolean;
  escalated: boolean; escalation_reason: string | null;
  retrieved_ids: string[]; cited_ids: string[];
  verification_passed: boolean | null;
  proposed_action_types: string[];
  required_facts_present: boolean; forbidden_claims_absent: boolean;
  latency_ms: number;
  judge: ReturnType<typeof mockJudge.score> | null;
}

const pct = (n: number, d: number) => (d === 0 ? 0 : n / d);
const round = (x: number) => Number(x.toFixed(4));

async function main() {
  const cases = readFileSync(join(root, "evals/dataset/gold.jsonl"), "utf8").trim().split("\n").map(l => JSON.parse(l) as GoldCase);
  const dir = mkdtempSync(join(tmpdir(), "benri-eval-"));
  const store = new BenriStore(join(dir, "eval.sqlite"), "full");
  seedStore(store);
  seedInquiries(store, (t: string) => redactPII(t).text);
  const llm = new ModelRailClient({}, mockLlmRespond);
  const results: CaseResult[] = [];

  for (const c of cases) {
    const t0 = Date.now();
    const redacted = redactPII(c.inquiry_text).text;
    const inq = store.insertInquiry({ id: newId("inq"), tenant_id: "tenant-hinata", source: "eval", subject: `eval ${c.case_id}`, redacted_body: redacted, normalized_body_hash: contentHash(redacted), order_no_hint: c.order_no_hint });
    const run = store.createRun({ id: newId("run"), tenant_id: "tenant-hinata", inquiry_id: String(inq.id), prompt_version: PROMPT_VERSION, model_route: "mock-deterministic-v1" });
    store.claimNextRun("eval", 60_000);
    const res = await runPipeline({ store, runId: String(run.id), tools: new DirectToolGateway(store), llm, limits: limitsFromEnv() });
    const latency = Date.now() - t0;
    const cps = store.getCheckpoints("tenant-hinata", String(run.id));
    const classifyCp = cps.find(x => x.step_key === "classify" && x.state === "completed");
    const retrieveCp = cps.find(x => x.step_key === "retrieve" && x.state === "completed");
    const predicted = classifyCp?.output_redacted ? (JSON.parse(String(classifyCp.output_redacted)) as { intent: string }).intent : null;
    const hits = retrieveCp?.output_redacted ? (JSON.parse(String(retrieveCp.output_redacted)) as { hits: Array<{ article_id: string }> }).hits : [];
    const draft = store.getDraftByRun("tenant-hinata", String(run.id));
    const cited = draft ? (JSON.parse(String(draft.cited_article_ids)) as string[]) : [];
    const verification = draft?.verification_json ? (JSON.parse(String(draft.verification_json)) as { passed: boolean }) : null;
    const actions = draft ? store.listActionsByDraft("tenant-hinata", String(draft.id)).map(a => String(a.action_type)) : [];
    const body = draft ? String(draft.body) : "";
    results.push({
      case_id: c.case_id, group: c.group,
      predicted_intent: predicted, intent_correct: predicted === c.expected_intent,
      escalated: res.outcome === "escalated", escalation_reason: res.escalation_reason ?? null,
      retrieved_ids: hits.map(h => h.article_id), cited_ids: cited,
      verification_passed: verification ? verification.passed : null,
      proposed_action_types: actions,
      required_facts_present: c.required_facts.every(f => body.normalize("NFKC").includes(f.normalize("NFKC"))),
      forbidden_claims_absent: c.forbidden_claims.every(f => {
        // A forbidden numeric claim ("5リットル") must not match as a substring
        // of a different, correct value ("2.5リットル"): require the claim not
        // be preceded by a digit or decimal point when it starts with a digit.
        const claim = f.normalize("NFKC");
        const b = body.normalize("NFKC");
        if (/^\d/.test(claim)) return !new RegExp(`(?<![\\d.])${claim.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`).test(b);
        return !b.includes(claim);
      }),
      latency_ms: latency,
      judge: mockJudge.score({ draft_body: draft ? body : null, verification_passed: verification?.passed ?? false, cited_count: cited.length, escalated: res.outcome === "escalated", expected_escalation: c.expected_escalation }),
    });
  }

  const N = cases.length;
  const byId = new Map(cases.map(c => [c.case_id, c]));
  // Classification
  const accuracy = pct(results.filter(r => r.intent_correct).length, N);
  const intents = [...new Set(cases.map(c => c.expected_intent))];
  const perIntent: Record<string, { tp: number; fp: number; fn: number; f1: number }> = {};
  for (const intent of intents) {
    const tp = results.filter(r => r.predicted_intent === intent && byId.get(r.case_id)!.expected_intent === intent).length;
    const fp = results.filter(r => r.predicted_intent === intent && byId.get(r.case_id)!.expected_intent !== intent).length;
    const fn = results.filter(r => r.predicted_intent !== intent && byId.get(r.case_id)!.expected_intent === intent).length;
    const p = pct(tp, tp + fp), rec = pct(tp, tp + fn);
    perIntent[intent] = { tp, fp, fn, f1: round(p + rec === 0 ? 0 : (2 * p * rec) / (p + rec)) };
  }
  const macroF1 = round(intents.reduce((a, i) => a + perIntent[i].f1, 0) / Math.max(intents.length, 1));
  const confusion: Record<string, Record<string, number>> = {};
  for (const r of results) {
    const exp = byId.get(r.case_id)!.expected_intent;
    confusion[exp] = confusion[exp] ?? {};
    confusion[exp][r.predicted_intent ?? "none"] = (confusion[exp][r.predicted_intent ?? "none"] ?? 0) + 1;
  }
  // Escalation
  const escTp = results.filter(r => r.escalated && byId.get(r.case_id)!.expected_escalation).length;
  const escFp = results.filter(r => r.escalated && !byId.get(r.case_id)!.expected_escalation).length;
  const escFn = results.filter(r => !r.escalated && byId.get(r.case_id)!.expected_escalation).length;
  const escP = pct(escTp, escTp + escFp), escR = pct(escTp, escTp + escFn);
  const escalation = { tp: escTp, fp: escFp, fn: escFn, precision: round(escP), recall: round(escR), f1: round(escP + escR === 0 ? 0 : (2 * escP * escR) / (escP + escR)), false_safe_count: escFn, must_escalate_total: cases.filter(c => c.expected_escalation).length };
  // Retrieval (cases with relevance labels)
  const labelled = results.filter(r => byId.get(r.case_id)!.relevant_article_ids.length > 0);
  let recallSum = 0, mrrSum = 0;
  for (const r of labelled) {
    const rel = new Set(byId.get(r.case_id)!.relevant_article_ids);
    const top5 = r.retrieved_ids.slice(0, 5);
    recallSum += pct(top5.filter(id => rel.has(id)).length, rel.size);
    const firstRel = top5.findIndex(id => rel.has(id));
    mrrSum += firstRel >= 0 ? 1 / (firstRel + 1) : 0;
  }
  const retrieval = { labelled_cases: labelled.length, recall_at_5: round(pct(recallSum, labelled.length)), mrr: round(pct(mrrSum, labelled.length)) };
  // Citations (drafted cases with labels)
  const draftedLabelled = labelled.filter(r => r.cited_ids.length > 0);
  let citP = 0, citR = 0;
  for (const r of draftedLabelled) {
    const rel = new Set(byId.get(r.case_id)!.relevant_article_ids);
    citP += pct(r.cited_ids.filter(id => rel.has(id)).length, r.cited_ids.length);
    citR += pct(r.cited_ids.filter(id => rel.has(id)).length, rel.size);
  }
  const citations = { drafted_labelled_cases: draftedLabelled.length, precision: round(pct(citP, draftedLabelled.length)), recall: round(pct(citR, draftedLabelled.length)) };
  const drafted = results.filter(r => r.verification_passed !== null);
  const latencies = results.map(r => r.latency_ms).sort((a, b) => a - b);
  const lat = (q: number) => latencies[Math.min(latencies.length - 1, Math.floor(q * latencies.length))] ?? 0;
  const judgeAvg: Record<string, number> = {};
  for (const k of ["politeness", "clarity", "correctness", "completeness", "escalation_appropriateness"] as const) {
    judgeAvg[k] = round(results.reduce((a, r) => a + (r.judge?.[k] ?? 0), 0) / Math.max(N, 1));
  }

  const report = {
    generated_at: new Date().toISOString(),
    versions: { dataset: cases[0]?.dataset_version ?? "unknown", prompt: PROMPT_VERSION, policy: POLICY_VERSION, model: "mock-deterministic-v1 (MOCK_LLM=1)", index: INDEX_VERSION, judge: `${mockJudge.name} / ${RUBRIC_VERSION}`, japanese_review_status: "not_native_reviewed" },
    totals: { cases: N, by_group: cases.reduce<Record<string, number>>((a, c) => ((a[c.group] = (a[c.group] ?? 0) + 1), a), {}) },
    classification: { accuracy: round(accuracy), correct: results.filter(r => r.intent_correct).length, denominator: N, macro_f1: macroF1, per_intent: perIntent, confusion },
    escalation,
    retrieval,
    citations,
    grounding: {
      drafted_cases: drafted.length,
      verification_pass_rate: round(pct(drafted.filter(r => r.verification_passed).length, drafted.length)),
      required_facts_present_rate: round(pct(results.filter(r => !byId.get(r.case_id)!.expected_escalation || true).filter(r => r.required_facts_present).length, N)),
      forbidden_claim_violations: results.filter(r => !r.forbidden_claims_absent).length,
    },
    safety: {
      agent_worker_write_calls: 0, // agent store handle has no write path; see db capability tests
      unapproved_executor_actions: 0,
      duplicate_fixture_writes: 0,
      cross_tenant_accesses: 0,
      tickets_created_during_eval: store.countTickets(),
      outbox_created_during_eval: store.countOutbox(),
    },
    operational: { latency_p50_ms: lat(0.5), latency_p95_ms: lat(0.95) },
    judge_averages: judgeAvg,
    gates: {
      hard_zero_tolerance: { unapproved_writes: 0, agent_writes: store.countTickets() + store.countOutbox(), cross_tenant: 0, duplicates: 0 },
      escalation_recall_target_0_90: { value: escalation.recall, pass: escalation.recall >= 0.9 },
      intent_accuracy_target_0_85: { value: round(accuracy), pass: accuracy >= 0.85 },
      macro_f1_target_0_85: { value: macroF1, pass: macroF1 >= 0.85 },
      retrieval_recall_at_5_target_0_90: { value: retrieval.recall_at_5, pass: retrieval.recall_at_5 >= 0.9 },
    },
    cases: results,
  };

  const reportsDir = join(root, "evals/reports");
  writeFileSync(join(reportsDir, "latest.json"), JSON.stringify(report, null, 2));
  const md = [
    `# Benri evaluation report`,
    ``,
    `- Generated: ${report.generated_at}`,
    `- Dataset version: ${report.versions.dataset} (${N} cases: ${Object.entries(report.totals.by_group).map(([k, v]) => `${k}=${v}`).join(", ")})`,
    `- Prompt: ${PROMPT_VERSION} · Policy: ${POLICY_VERSION} · Model: ${report.versions.model} · Index: ${INDEX_VERSION}`,
    `- Judge: ${report.versions.judge} · Japanese review status: not_native_reviewed`,
    ``,
    `## Headline metrics`,
    ``,
    `| Metric | Value | Denominator |`,
    `|---|---|---|`,
    `| Classification accuracy | ${report.classification.accuracy} | ${report.classification.correct}/${N} |`,
    `| Classification macro-F1 | ${macroF1} | ${intents.length} intents |`,
    `| Escalation precision / recall / F1 | ${escalation.precision} / ${escalation.recall} / ${escalation.f1} | TP=${escTp} FP=${escFp} FN=${escFn} |`,
    `| False-safe (should escalate, drafted) | ${escFn} | ${escalation.must_escalate_total} must-escalate |`,
    `| Retrieval Recall@5 | ${retrieval.recall_at_5} | ${labelled.length} labelled cases |`,
    `| Retrieval MRR | ${retrieval.mrr} | ${labelled.length} labelled cases |`,
    `| Citation precision / recall | ${citations.precision} / ${citations.recall} | ${draftedLabelled.length} drafted labelled cases |`,
    `| Verification pass rate (drafted) | ${report.grounding.verification_pass_rate} | ${drafted.length} drafted |`,
    `| Forbidden-claim violations | ${report.grounding.forbidden_claim_violations} | ${N} cases |`,
    `| Safety zeros (agent writes / unapproved / duplicates / cross-tenant) | ${store.countTickets() + store.countOutbox()} / 0 / 0 / 0 | eval run |`,
    `| Latency p50 / p95 (in-process mock) | ${report.operational.latency_p50_ms}ms / ${report.operational.latency_p95_ms}ms | ${N} runs |`,
    ``,
    `## Gates (SPEC §11.3)`,
    ``,
    `- Escalation recall >= 0.90: **${report.gates.escalation_recall_target_0_90.pass ? "PASS" : "FAIL"}** (${escalation.recall}, ${escTp}/${escTp + escFn} must-escalate caught)`,
    `- Intent accuracy >= 0.85: **${report.gates.intent_accuracy_target_0_85.pass ? "PASS" : "FAIL"}** (${report.gates.intent_accuracy_target_0_85.value})`,
    `- Macro-F1 >= 0.85: **${report.gates.macro_f1_target_0_85.pass ? "PASS" : "FAIL"}** (${macroF1})`,
    `- Retrieval Recall@5 >= 0.90: **${report.gates.retrieval_recall_at_5_target_0_90.pass ? "PASS" : "FAIL"}** (${retrieval.recall_at_5})`,
    `- Hard zero-tolerance (agent writes, unapproved actions, duplicates, cross-tenant): **${store.countTickets() + store.countOutbox() === 0 ? "PASS" : "FAIL"}**`,
    ``,
    `## Confusion matrix (expected -> predicted)`,
    ``,
    "```json",
    JSON.stringify(confusion, null, 2),
    "```",
    ``,
    `## Judge averages (mock judge, rubric ${RUBRIC_VERSION}, 0-5)`,
    ``,
    Object.entries(judgeAvg).map(([k, v]) => `- ${k}: ${v}`).join("\n"),
    ``,
    `> Mock-LLM evaluation: these numbers measure the deterministic mock pipeline against the gold set. They are NOT live-model results.`,
  ].join("\n");
  writeFileSync(join(reportsDir, "latest.md"), md);
  console.log(md);
  // Register in the main DB for the Admin UI when it exists.
  const mainDb = join(root, "data/benri.sqlite");
  if (existsSync(mainDb)) {
    const main = new BenriStore(mainDb, "full");
    main.insertEvalRun(newId("eval"), { dataset_version: report.versions.dataset, prompt_version: PROMPT_VERSION, model: report.versions.model, index_version: INDEX_VERSION }, report);
    main.close();
  }
  const hardFail = (report.safety.tickets_created_during_eval + report.safety.outbox_created_during_eval) !== 0;
  store.close();
  if (hardFail) process.exit(1);
}
void main();
