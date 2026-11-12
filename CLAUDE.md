# CLAUDE.md — rules for the coding agent on this repo

Spec: SPEC.md (v2.0, 2026-11-12) is the source of truth. If a task conflicts with it, stop and flag it.

- Stack: TypeScript, Node.js LTS (pinned), Fastify (api), Next.js (web), PostgreSQL + pgvector (Drizzle schema/migrations), MCP TypeScript SDK.
- Local verification on this machine: no Docker/PostgreSQL available. The portable store
  implementation (node:sqlite) is the locally verified path; the PostgreSQL/Drizzle path is
  provided for Compose deployments and must be labelled unverified until run against real PG.
- Trust boundary (SPEC §3.3, §6): the agent worker is read-only — it must NEVER get write
  credentials, ticket/outbox write access, or a route to the executor. The executor accepts
  only persisted, hash-verified, approved action IDs. Never weaken this for convenience.
- Proposed actions come from deterministic intent/policy templates, never from free-form
  LLM tool calls. The model output contains no action proposals (SPEC §8.5).
- Tests before declaring done: run the vitest suites and report real output.
- Do not invent KB policies, prices, or dates. Fixture facts live in fixtures/ only.
- PII: persist only redacted bodies; never log unmasked emails, phone numbers, addresses,
  or order numbers (SPEC §6.4).
- No unmeasured claims in README/docs/CV copy. Numbers come from the eval harness only.
- Complaints are human-led in v1: internal handoff summary only, no customer-facing draft.
- Small diffs: one task per change set. If a change touches >10 files, split it and say why.
- After each session, add an AGENT-LOG.md entry the same day (SPEC §14.2), honestly naming
  the agent used (Muse / Claude Code / other) and any corrections or rejections.
