# Benri Agent — Architecture

Implements SPEC.md v2.0. One-line definition: Benri turns Japanese customer
inquiries into evidence-backed response drafts and explicit action proposals;
an authenticated operator reviews the exact content before a separate executor
writes anything to the demo system.

## Components

| Component | Path | Role |
|---|---|---|
| API | `apps/api` (Fastify) | Auth (session tokens, hashed at rest), tenant-scoped endpoints, approval transaction. Never runs model reasoning. |
| Agent worker | `apps/agent-worker` | Claims runs (lease + heartbeat), executes the 7-step pipeline, persists checkpoints. Runs with an **agent-role store handle** that structurally rejects writes to `tickets`, `outbox_messages`, `approvals`, `execution_jobs`. |
| Executor worker | `apps/executor-worker` | Claims approved execution jobs, verifies payload hashes, performs each fixture action exactly once. No LLM client. Ambiguous outcomes become `unknown`, never blind-retried. |
| Web UI | `apps/web` (Next.js) | Queue / run detail / review / escalations / admin. Japanese default, EN toggle. |
| KB MCP | `packages/mcp-kb` | Read-only `search_kb`, `get_article` over stdio (MCP v2 packages, smoke-tested in this runtime). |
| CRM MCP | `packages/mcp-crm` | Read-only `lookup_order` over synthetic fixtures. |
| Ticket MCP | `packages/mcp-ticket` | Write-only, executor-hosted only: `create_ticket`, `add_ticket_note`. Takes `approved_action_id` only; loads and hash-verifies the canonical payload server-side. |
| agent-core | `packages/agent-core` | Pipeline, deterministic policy, verifier, approval transaction, mock LLM, redaction. |
| retrieval | `packages/retrieval` | Japanese lexical retrieval: NFKC + particle-split char bigrams, IDF-weighted content-coverage evidence gate (intent-aware). Dense retrieval disabled (see below). |
| db | `packages/db` | Drizzle PostgreSQL schema + migration SQL + role SQL (provided, **unverified** — no PG here) and the portable `node:sqlite` store (locally verified) implementing the same repository contracts with capability-scoped handles. |
| modelrail-client | `packages/modelrail-client` | OpenAI-compatible chat client (timeouts, usage parsing, typed `GATEWAY_UNAVAILABLE`) + mock mode. |
| observability | `packages/observability` | OTel-shaped spans (`benri.run` + child spans per step/tool), in-memory collector, async optional OTLP/HTTP export to Kairo; exporter failure never fails a run. |
| japanese-quality | `packages/japanese-quality` | Deterministic format/register checks (signature, banned phrases, casual endings). Mechanical only — not a fluency guarantee. |

## Trust boundary

The process that interprets untrusted customer text (agent worker) has no
write capability: separate store role, read-only MCP servers only, no route
to the executor. Approval is bound to the exact draft revision and per-action
canonical payload hashes, persisted with the execution jobs in one
transaction. The executor reloads and hash-verifies before running;
actions are single-use with atomic state transitions and idempotency keys.

## Key decisions and deviations

- **Embeddings spike (SPEC §19.1):** the sibling ModelRail codebase exposes
  no embeddings endpoint (verified by source search). Dense retrieval is
  therefore **disabled**; Benri ships lexical-only retrieval. RRF helper is
  present but unused. Recorded in VERIFICATION-REPORT.md.
- **Tokenizer (SPEC §8.2):** kuromoji was not adopted; the explicitly
  selected tokenizer is dictionary-free: NFKC + particle-split character
  bigrams. Retrieval is called **lexical retrieval**, never BM25.
- **MCP SDK (SPEC §19.3):** minimal smoke test of `@modelcontextprotocol/server`
  v2.4.0 + `@modelcontextprotocol/client` v2.4.0 over stdio passed in this
  Node 24 runtime (echo tool listed + called). v2 is used; no v1 fallback
  needed.
- **Persistence:** PostgreSQL/Drizzle is the Compose path (unverified here);
  all business logic runs against repository contracts exercised by the
  sqlite store in tests, evals, and the live smoke.
- **Runtime:** processes run TypeScript via `tsx` (no separate build step)
  in local/dev mode; the web app is a real Next.js production build.

## Data flow

`POST /inquiries/:id/runs` → agent worker claims (lease) → steps 0–7 with
checkpoints → draft at `awaiting_review` + deterministic proposed actions →
operator approves selected actions (one transaction: approval + jobs) →
executor claims job → ticket MCP / outbox adapter → fixture writes →
audit events + spans at every transition.
