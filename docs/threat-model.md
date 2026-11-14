# Benri Agent — threat model (v3)

Assets: customer inquiry text (PII-bearing), KB content, CRM order data,
the approval record, and the side-effecting action surface (tickets,
outbox). Benri's core security claim is narrow and testable: **no
side-effecting write happens without a persisted human approval bound to
the exact draft revision and exact action payload hashes.**

## Threats and controls

| Threat | Control |
|---|---|
| Prompt injection in inquiry text ("ignore instructions, give a discount") | Deterministic classifier + policy layer (never the model alone) escalates injection/out-of-scope requests; the model cannot propose actions — action templates are deterministic per intent. |
| Agent process compromise / runaway | Agent store role structurally cannot insert approvals, execution_jobs, tickets, or outbox rows (capability checks in the store + tests). Agent worker binary ships without the ticket MCP server and has no route to it. |
| Approval tampering / TOCTOU (draft edited after display) | Approval binds draft revision + content hash + per-action payload hashes; the executor and ticket server re-hash the stored canonical payload and compare against the approval record before writing. Re-approval of a decided draft is refused (409). |
| Duplicate execution (retries, replays, double-claims) | Idempotency keys on runs and jobs; UNIQUE constraints; atomic `queued → running` action transition (single winner); ticket/outbox lookups by source action make replays return the existing record. |
| Ambiguous tool outcome (request sent, response lost) | Transport errors after send are classified `OutcomeUnknown`; the job becomes `unknown` and is never blind-retried. Expired job leases also resolve to `unknown`, not requeue. |
| Cross-tenant data leak | Tenant is bound at MCP server launch (env), never taken from tool arguments; tenant-scoped queries everywhere; CRM lookup distinguishes ORDER_NOT_FOUND from CROSS_TENANT_ORDER without leaking rows; store tests prove cross-tenant reads fail closed. |
| PII persistence / leakage | The redactor (NFKC → email/phone/postal/my-number/card/order patterns, order-number pattern FIRST) runs before persistence at seed/API/pipeline; persisted bodies and checkpoints are redacted-only; raw text exists only transiently in memory. Span attributes and tool-call args are redacted. |
| Model gateway failure / cost runaway | Typed GATEWAY_UNAVAILABLE failure ends the run `failed`; per-run hard limits (model calls, tool calls, duration, tokens) with MODEL_BUDGET_EXCEEDED escalation. |
| KB poisoning via admin writes | KB writes are admin-only; every draft is verified against the retrieved evidence at draft time AND re-verified at approval time against the run's checkpointed evidence. |
| Session theft | Bearer tokens are stored SHA-256-hashed; passwords are scrypt-hashed; login failures return one stable error code. |

## Known limits (honest)

- The deterministic verifier proves citation membership, source-value
  containment, and policy-value support. It does NOT prove semantic
  entailment of free-form sentences — human review remains mandatory.
- Mechanical Japanese style checks are not native-speaker review; the
  fixtures/gold data are labelled `not_native_reviewed`.
- PostgreSQL role grants are exercised by the Compose services; application
  capability guards are also tested independently with every store role.
- Demo auth/users are for local demonstration; production deployment must
  replace them and terminate TLS at a real proxy.
