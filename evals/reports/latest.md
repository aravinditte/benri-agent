# Benri evaluation report

- Generated: 2026-11-12T10:25:10Z
- Dataset version: 1.0.0 (50 cases: routine=24, edge=10, must-escalate=8, adversarial=8)
- Prompt: benri-prompt-v3.0.0 · Policy: benri-policy-v1.0.0 · Model: mock-deterministic-v1 (MOCK_LLM=1) · Index: lexical-v3-kagome-ipa-1.2.6
- Judge: mock-deterministic-judge / rubric-v1.0.0 · Japanese review status: not_native_reviewed

## Kagome dictionary benchmark (§8.2)

| Dictionary | Index version | Recall@5 | MRR | Labelled cases |
|---|---|---|---|---|
| kagome-ipa-1.2.6 | lexical-v3-kagome-ipa-1.2.6 | 0.9375 | 0.8958 | 40 |
| kagome-unidic-1.2.6 | lexical-v3-kagome-unidic-1.2.6 | 0.9375 | 0.8917 | 40 |

Chosen: **kagome-ipa-1.2.6** (higher Recall@5, MRR tiebreak).

## Headline metrics

| Metric | Value | Denominator |
|---|---|---|
| Classification accuracy | 0.9800 | 49/50 |
| Classification macro-F1 | 0.9753 | 9 intents |
| Escalation precision / recall / F1 | 1.0000 / 1.0000 / 1.0000 | TP=16 FP=0 FN=0 |
| False-safe (should escalate, drafted) | 0 | 16 must-escalate |
| Retrieval Recall@5 | 0.9625 | 40 labelled cases |
| Retrieval MRR | 0.8875 | 40 labelled cases |
| Citation precision / recall | 0.7941 / 0.9265 | 34 drafted labelled cases |
| Verification pass rate (drafted) | 1.0000 | 34 drafted |
| Forbidden-claim violations | 0 | 50 cases |
| Latency p50 / p95 (in-process mock) | 704ms / 980ms | 50 runs |

## Gates (SPEC §11.3)

- Escalation recall >= 0.90: **PASS** (1.0000, 16/16 must-escalate caught)
- Intent accuracy >= 0.85: **PASS** (0.9800)
- Macro-F1 >= 0.85: **PASS** (0.9753)
- Retrieval Recall@5 >= 0.90: **PASS** (0.9625)
- Hard zero-tolerance (agent writes, unapproved actions, duplicates, cross-tenant): **PASS** (0 tickets / 0 outbox created during eval; capability tests prove the agent handle cannot write them)

## Confusion matrix (expected -> predicted)

```json
{
  "account": {
    "account": 6
  },
  "cancellation": {
    "cancellation": 4
  },
  "complaint": {
    "complaint": 3
  },
  "delivery_status": {
    "delivery_status": 11
  },
  "invoice_request": {
    "invoice_request": 4
  },
  "other": {
    "other": 4,
    "price_stock": 1
  },
  "price_stock": {
    "price_stock": 4
  },
  "product_usage": {
    "product_usage": 7
  },
  "return_exchange": {
    "return_exchange": 6
  }
}
```

## Judge averages (mock judge, rubric rubric-v1.0.0, 0-5)

- politeness: 3.4000
- clarity: 3.4000
- correctness: 3.4000
- completeness: 2.7200
- escalation_appropriateness: 5.0000

> Mock-LLM evaluation: these numbers measure the deterministic mock pipeline against the gold set. They are NOT live-model results.
