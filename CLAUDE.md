# CLAUDE.md — rules for the coding agent on this repo

Spec: SPEC.md (v3.0, Go + TypeScript, 2026-11-12) is the source of truth. If a task conflicts with it, stop and flag it.

- Stack: Go backend (single module; cmd/ entry points, internal/ packages; net/http + chi;
  pgx/v5 + sqlc; goose migrations; official MCP Go SDK; Kagome v2 tokenizer) and
  TypeScript/Next.js only in web/. NO Node.js backend. API contract is api/openapi.yaml —
  generate Go and TypeScript types from it; never hand-edit generated code.
- Local verification on this machine: no Docker/PostgreSQL. The locally verified store path
  is SQLite (pure-Go driver) implementing the same repository contracts with
  capability-scoped role handles; the PostgreSQL/pgx/sqlc path ships for Compose
  deployments and must be labelled unverified until run against real PG.
- Trust boundary (SPEC §3.3, §6, §9): the agent worker can create run/checkpoint/draft/
  proposed-action records ONLY. It cannot approve proposals, enqueue execution jobs,
  write tickets/outbox, or invoke the executor. The executor cannot write tickets
  directly — only the ticket MCP server inserts tickets, after validating the persisted
  approval + payload hash. Never weaken this for convenience.
- Proposed actions come from deterministic intent/policy templates, never from free-form
  LLM output. Model output contains no action proposals (SPEC §8.5).
- Go hygiene: gofmt + go vet + go test ./... (+ -race where supported) before declaring
  done; report real output. Web: typecheck + build before declaring done.
- Do not invent KB policies, prices, or dates. Fixture facts live in fixtures/ only.
- PII: persist only redacted bodies; never log unmasked emails, phone numbers, addresses,
  or order numbers (SPEC §6.4).
- No unmeasured claims in README/docs/CV copy, and do NOT claim Node.js backend experience
  from this project (SPEC §0.2) — the backend is Go; TypeScript is the web UI only.
  Numbers come from the eval harness only.
- Complaints are human-led in v1: internal handoff summary only, no customer-facing draft.
- After each session, add an AGENT-LOG.md entry the same day (SPEC §14.2), honestly naming
  the agent used (Muse / Claude Code / other) and any corrections or rejections.
