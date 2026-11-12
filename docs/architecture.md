# Benri Agent — architecture (v3, Go + TypeScript)

Benri Agent is an approval-gated support copilot for Japanese e-commerce
support teams. An AI agent drafts replies and proposes side-effecting
actions; a human operator approves exact drafts and exact actions; a
separate executor performs only what was approved. The backend is Go; the
operator UI is Next.js (TypeScript) and exists only in `web/`.

## Processes (one Go module, `cmd/`)

| Process | Store role | Responsibility |
|---|---|---|
| `cmd/api` | `api` | HTTP API (chi + oapi-codegen from `api/openapi.yaml`). Approvals + execution-job insertion. Cannot write tickets/outbox. |
| `cmd/agent-worker` | `agent` | Claims queued runs, executes the pipeline (§5.1). Can create runs/steps/drafts/PROPOSED actions only. Cannot approve, enqueue jobs, or write tickets/outbox. |
| `cmd/executor-worker` | `executor` | Claims approved execution jobs. Inserts outbox records. Cannot insert tickets directly. |
| `cmd/mcp-kb` | `kb-mcp` | Read-only KB search/get over MCP stdio. Tenant bound at launch. |
| `cmd/mcp-crm` | `crm-mcp` | Read-only order lookup over MCP stdio. Tenant bound at launch. |
| `cmd/mcp-ticket` | `ticket-mcp` | The ONLY ticket writer. Every call re-loads the persisted approval and validates the payload hash. |
| `cmd/eval` | (throwaway DB) | Kagome dictionary benchmark + gold-set evaluation CLI. |
| `cmd/smoke` | — | Live end-to-end smoke orchestrator (binaries + MCP on). |

## Trust boundary (§3.3/§9)

Three independent mechanisms, each tested:

1. **Capability-scoped store handles** — every process opens the store with
   a role; write methods check the role before touching approvals,
   execution_jobs, tickets, ticket_notes, outbox tables
   (`internal/store`, `guardWrite`). Tests prove each prohibition.
2. **Approval binding** — approval records the exact draft revision +
   content hash + selected action ids; each action's canonical payload is
   SHA-256 hashed. Approval re-runs the deterministic verifier first.
3. **Executor re-validation** — before any write, the executor and the
   ticket MCP server re-load the persisted approval and re-hash the stored
   canonical payload (`executor.LoadVerifiedAction`). The ticket tool also
   performs the atomic `queued → running` action transition, so duplicate
   or replayed calls collapse to one ticket.

Ticket-action state is owned by the ticket server; the executor owns only
`send_demo_reply` transitions.

## Data path

- **SQLite** (`modernc.org/sqlite`, pure Go) is the locally verified store.
- **PostgreSQL** path: goose migrations (`db/migrations/`, incl. per-role
  grants in `00002_role_grants.sql`), sqlc queries (`db/queries/` →
  `internal/store/dbgen`, generated + committed), pgx/v5 pool handle in
  `internal/store/pg.go`. **Status: provided, UNVERIFIED** — no PostgreSQL
  on the build machine; the PgStore method bodies return an explicit
  unverified error instead of pretending to work.

## Retrieval (§8)

Lexical-only. Kagome v2 tokenization (Search mode) after NFKC
normalization; IDF-weighted content-coverage scoring with an intent-aware
gate (thresholds 0.18 / 0.12, carried from v2 calibration). The dictionary
was chosen by benchmark on the labelled gold corpus: IPA vs UniDic tied on
Recall@5 (0.9375); IPA won on MRR (0.8958 vs 0.8917) → IPAdic is the
default; the index version names it: `lexical-v3-kagome-ipa-1.2.6`.
Dense retrieval is DISABLED: the ModelRail capability spike found no
embeddings endpoint (chat/completions only). The RRF fusion helper ships
unused, by design.

## Model gateway (§10.1)

Typed ModelRail client (`internal/modelrail`): timeouts, bounded response
size, usage parsing, typed `GATEWAY_UNAVAILABLE` failures. Mock mode
(`MOCK_LLM=1` or no base URL) is deterministic and clearly labelled in
`/health`, the UI, and every report. The mock responder
(`internal/agent.MockRespond`) implements the same structured contracts a
live model would: CLASSIFY → JSON classification, DRAFT → JSON draft with
citations + factual references.

## Observability (§10.2)

OpenTelemetry Go SDK: root span `benri.run` + child spans per step, W3C
trace context, async OTLP/HTTP export to Kairo when configured (export
failure never fails a run), plus an always-on in-memory span recorder for
tests/local debugging. Span attributes never contain raw inquiry text,
PII, or full order numbers.

## Web (`web/`)

Next.js operator UI (Japanese-first, EN toggle). Its API types are
generated from the canonical contract: `openapi-typescript` →
`web/lib/api/schema.d.ts` (committed). `make generate` regenerates the Go
server stubs, sqlc output, and TS types; `make generate-check` fails on
any drift (exercised locally, wired into CI).
