# Benri judge rubric — rubric-v1.0.0

Deterministic mock judge used in CI (`cmd/eval`). Scores are a quality
signal, never a security gate (SPEC §11.4). A live judge is a separate,
non-CI command and is not part of this delivery.

Dimensions (0–5 each):

- **politeness** — 5 when the draft uses です/ます/ございます register AND
  carries the required signature (ひなたストア カスタマーサポート); else 2.
- **clarity** — 5 when the draft body length is 80–1200 characters; else 3.
- **correctness** — 5 when the deterministic verifier passed; else 1.
- **completeness** — 4 when the draft cites ≥1 article; else 1.
- **escalation_appropriateness** — 5 when the run's escalate/draft decision
  matches the gold expectation; else 0. For escalated cases this is the
  only non-zero dimension.

Japanese fixtures and gold cases are **not native-reviewed**
(`japanese_review_status: not_native_reviewed`).
