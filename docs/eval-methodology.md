# Benri Agent — evaluation methodology (v3)

## Gold dataset

`evals/dataset/gold.jsonl` — 50 cases (24 routine / 10 edge / 8
must-escalate / 8 adversarial), dataset version 1.0.0, **byte-identical to
the v2 dataset** (sha256 recorded in VERIFICATION-REPORT.md). Each case
labels the expected intent, urgency, escalation decision + reason code,
relevant article ids, required facts, forbidden claims, and allowed
actions. Japanese text is synthetic and **not native-reviewed**.

## Runner

`cmd/eval` (Go) exercises the production `internal/` packages — the real
pipeline, real store, real verifier — against a throwaway seeded SQLite
database, with the deterministic mock LLM and the direct tool gateway.
Reports (JSON + Markdown) land in `evals/reports/` with dataset, prompt,
policy, model, and index versions and explicit denominators for every
metric. The eval also registers its report into the main database (when
present) for the Admin UI.

## Dictionary benchmark (§8.2)

Before the gold run, the CLI benchmarks Kagome IPA vs UniDic (Search
mode) over the 40 labelled cases: Recall@5 and MRR per dictionary. The
winner runs the gold set and is named in the index version string. 2026-11-12
result: Recall@5 tied at 0.9375; IPA won on MRR (0.8958 vs 0.8917) →
IPAdic.

## Metrics

- Classification accuracy + macro-F1 + confusion matrix (predicted intent
  read from the persisted classify checkpoint).
- Escalation precision/recall/F1 + false-safe count (a false-safe — a case
  that should escalate but was drafted — is the worst error class here).
- Retrieval Recall@5 + MRR over the 40 labelled cases.
- Citation precision/recall over drafted labelled cases.
- Verification pass rate over drafted cases; required-facts presence;
  forbidden-claim violations with a digit-boundary guard (a forbidden
  "5リットル" must not match inside a correct "2.5リットル").
- Mock-judge averages (rubric `rubric-v1.0.0`, `evals/rubrics/`): a
  quality signal, never a security gate.
- Latency p50/p95 of the in-process mock pipeline — NOT production
  figures.

## Gates (§11.3)

Escalation recall ≥ 0.90 · intent accuracy ≥ 0.85 · macro-F1 ≥ 0.85 ·
Recall@5 ≥ 0.90 · hard zeros for agent writes, unapproved executor
actions, duplicate fixture writes, and cross-tenant accesses. The eval
CLI exits non-zero when any gate fails; CI runs it as the mock eval gate.

## Negative controls (§11.5)

Implemented as Go tests/assertions: retrieval disabled → the same routine
inquiry escalates INSUFFICIENT_EVIDENCE; gateway down → bounded
GATEWAY_UNAVAILABLE failure; crash after a step → resume completes
without duplicating steps; ticket tool refuses unapproved actions;
ambiguous ticket outcome → job `unknown`, never retried.

## Honesty

Mock-LLM numbers measure the deterministic mock pipeline. They are not
live-model results, and no report in this repo presents them as such.
