# Benri Agent — Threat Model

Scope: v1 demo system, synthetic fixture data only. Controls reference SPEC §6.3.

| Threat | Control | Evidence |
|---|---|---|
| Prompt injection in inquiry text | Inquiry treated as untrusted data; fixed pipeline; agent worker has **no write capability** (store role + read-only MCP only); injection patterns routed to escalation; regression cases in gold set (G-043…G-050) | Eval: 8/8 adversarial cases produce no write; pipeline injection test |
| Injection in retrieved KB content | KB chunks are data; only allowlisted tools/schemas exist; the model cannot propose actions (deterministic templates only) | agent-core design; gold tool-output-injection case escalates |
| Model invents refund/discount/date/stock promise | Evidence-bounded drafting; structured factual references; deterministic verifier checks every policy-sensitive value against evidence/order; banned-phrase list; mandatory human review | Verifier tests; eval forbidden-claim violations = 0 |
| Replayed approval | Exact revision + payload hashes, single-use atomic action transitions, unique idempotency keys | Executor tests: replay creates 0 duplicates |
| Draft edited after approval | Immutable revisions; edit creates a new revision, supersedes the old, invalidates prior verification/approval; approve endpoint re-verifies and binds to revision | API revision endpoint; approval REVISION_MISMATCH test |
| Cross-tenant access | Tenant-scoped repository methods everywhere; tenant derived from session, never from request body; cross-tenant reads return nothing | DB store tests; pipeline sakura-tenant test; eval safety zeros |
| PII in logs/traces/persistence | Redaction (NFKC + JP phone/postal/My-Number-like/order-number patterns) before persistence and before model calls; only `redacted_body` stored; span attributes carry IDs only | Redaction tests; API test proves phone not persisted |
| Worker crash during execution | Leases + checkpoints for runs (resume, no repeated completed steps); transactional fixture writes + idempotency for actions; expired running job → `unknown`, operator reconciliation, never blind retry | Crash-resume test; unknown-outcome executor test |
| Public demo abuse | Fixture-only, mock-LLM-only, no arbitrary public submission in production mode, demo credentials documented as demo | API DEMO_MODE guard |

## Known limitations

- Redaction is a limited pattern detector, not proof of complete Japanese
  PII removal (SPEC §6.4).
- The verifier checks citations, structured facts, numbers/dates and style
  mechanically; it does **not** prove semantic entailment of free-form
  sentences. Human approval remains mandatory.
- Capability separation on the sqlite path is enforced by the store-handle
  role in-process; on PostgreSQL it is additionally enforced by DB role
  grants (`packages/db/src/pg-roles.sql`, unverified).
- Japanese fixture content is synthetic and **not native-reviewed**.
