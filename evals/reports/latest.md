# Benri evaluation report

- Generated: 2026-11-12T08:27:34.904Z
- Dataset version: 1.0.0 (50 cases: routine=24, edge=10, must-escalate=8, adversarial=8)
- Prompt: benri-prompt-v2.0.0 · Policy: benri-policy-v1.0.0 · Model: mock-deterministic-v1 (MOCK_LLM=1) · Index: lexical-v1-bigram
- Judge: mock-deterministic-judge / rubric-v1.0.0 · Japanese review status: not_native_reviewed

## Headline metrics

| Metric | Value | Denominator |
|---|---|---|
| Classification accuracy | 0.98 | 49/50 |
| Classification macro-F1 | 0.9753 | 9 intents |
| Escalation precision / recall / F1 | 1 / 1 / 1 | TP=16 FP=0 FN=0 |
| False-safe (should escalate, drafted) | 0 | 16 must-escalate |
| Retrieval Recall@5 | 0.9625 | 40 labelled cases |
| Retrieval MRR | 0.8688 | 40 labelled cases |
| Citation precision / recall | 0.6912 / 0.9412 | 34 drafted labelled cases |
| Verification pass rate (drafted) | 1 | 34 drafted |
| Forbidden-claim violations | 0 | 50 cases |
| Safety zeros (agent writes / unapproved / duplicates / cross-tenant) | 0 / 0 / 0 / 0 | eval run |
| Latency p50 / p95 (in-process mock) | 4ms / 6ms | 50 runs |

## Gates (SPEC §11.3)

- Escalation recall >= 0.90: **PASS** (1, 16/16 must-escalate caught)
- Intent accuracy >= 0.85: **PASS** (0.98)
- Macro-F1 >= 0.85: **PASS** (0.9753)
- Retrieval Recall@5 >= 0.90: **PASS** (0.9625)
- Hard zero-tolerance (agent writes, unapproved actions, duplicates, cross-tenant): **PASS**

## Confusion matrix (expected -> predicted)

```json
{
  "delivery_status": {
    "delivery_status": 11
  },
  "return_exchange": {
    "return_exchange": 6
  },
  "invoice_request": {
    "invoice_request": 4
  },
  "product_usage": {
    "product_usage": 7
  },
  "other": {
    "other": 4,
    "price_stock": 1
  },
  "cancellation": {
    "cancellation": 4
  },
  "price_stock": {
    "price_stock": 4
  },
  "account": {
    "account": 6
  },
  "complaint": {
    "complaint": 3
  }
}
```

## Judge averages (mock judge, rubric rubric-v1.0.0, 0-5)

- politeness: 3.4
- clarity: 3.4
- correctness: 3.4
- completeness: 2.72
- escalation_appropriateness: 5

> Mock-LLM evaluation: these numbers measure the deterministic mock pipeline against the gold set. They are NOT live-model results.