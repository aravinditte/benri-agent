# Benri Agent — Evaluation Methodology

Implements SPEC §11. The harness is independent of the web UI and runs with
deterministic mocks (`MOCK_LLM=1`) so CI is reproducible.

## Dataset

`evals/dataset/gold.jsonl` — 50 cases, dataset version 1.0.0:
24 routine / 10 edge / 8 must-escalate / 8 adversarial. Each case carries
expected intent + urgency, escalation boolean + reason code, relevant
article IDs, required/forbidden facts, expected allowed actions, and
adversarial flags. All content is synthetic for the fictional Hinata Store.
**Japanese review status: not_native_reviewed** (no reviewer available);
this is recorded in the dataset README and in every report.

## Runner

`npm run eval` (`evals/runner/src/run-eval.ts`) seeds a throwaway database
from the same fixtures, runs the real agent pipeline per case (mock LLM,
direct tool gateway — the same code path as the MCP gateway, which is
covered separately by stdio integration tests), and computes:

- Classification: accuracy, macro-F1, per-intent P/R/F1, confusion matrix.
- Escalation: precision/recall/F1 with raw TP/FP/FN counts and the
  false-safe count (must-escalate cases that received a draft).
- Retrieval: Recall@5 and MRR over the 40 cases with relevance labels.
- Citations: precision/recall over drafted labelled cases. Note: gold
  relevance labels are mostly single-article, while a correct draft may
  legitimately cite a sibling procedure article — citation precision is
  therefore a strict lower bound, reported as measured.
- Grounding: verification pass rate, required-fact presence, forbidden-claim
  violations (numeric claims are matched with a digit-boundary rule so a
  correct "2.5リットル" is not counted as the forbidden "5リットル").
- Safety: agent-worker writes, unapproved executor actions, duplicate
  writes, cross-tenant accesses — all must be zero (hard gates).
- Operational: in-process mock latency p50/p95 (not a production figure).
- Judge: mock deterministic judge (`evals/judges/judge.ts`, rubric v1.0.0)
  scores politeness/clarity/correctness/completeness/escalation
  appropriateness. Live judge runs are a separate non-CI command by design.

Reports are written to `evals/reports/latest.json` and `latest.md` with
dataset/prompt/policy/model/index/judge versions and all denominators.

## Gates (SPEC §11.3)

Hard zero-tolerance: agent writes, unapproved actions, duplicates,
cross-tenant — runner exits non-zero if violated. Soft targets (reported,
not hidden): escalation recall ≥ 0.90, intent accuracy ≥ 0.85, macro-F1
≥ 0.85, retrieval Recall@5 ≥ 0.90.

## Negative controls (SPEC §11.5)

Implemented as reproducible tests, not as a manufactured failing CI claim:
retrieval disabled → safe escalation; gateway down → bounded
`GATEWAY_UNAVAILABLE`; Kairo exporter down → run still completes; worker
crash after a checkpoint → resume without duplicated steps; executor
ambiguous outcome → `unknown`, no blind retry; malformed tool output hook
in the pipeline. See the vitest suites.

## Calibration note

The lexical evidence gate (content-coverage thresholds 0.18 / 0.12
intent-matched) and classifier keyword sets were calibrated against this
labelled set, as SPEC §5.4/§8.3 prescribe. The overseas-shipping rule is
evidence-driven: a scope term (海外) that no retrieved article mentions has
no grounded answer and escalates as `NO_RELEVANT_KB_EVIDENCE`.
