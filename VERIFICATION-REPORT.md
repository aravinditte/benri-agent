# Benri Agent — Verification Report

Build date: 2026-11-12 · Environment: Node v24.22.1, npm 10.9.4, Linux VM
(no Docker, no PostgreSQL, no pnpm on this machine).
Verified path: portable `node:sqlite` store + deterministic mock LLM
(`MOCK_LLM=1`), MCP gateways enabled for the live smoke.

## Commands and results (actual outputs)

| Check | Command | Result |
|---|---|---|
| Typecheck (all packages/apps except web) | `npx tsc --noEmit -p tsconfig.json` | **Clean, 0 errors** |
| Web typecheck | `cd apps/web && npx tsc --noEmit` | **Clean** |
| Web build | `cd apps/web && npm run build` | **Pass** (7 routes: /, /login, /inquiries/[id], /escalations, /admin + 404) |
| Tests | `npx vitest run` | **43/43 passed, 9 files** (core suites run twice with identical results) |
| Evaluation | `npm run eval` | Report below; committed at `evals/reports/latest.json` + `latest.md` |
| Live smoke | `npx tsx scripts/smoke.ts` | **SMOKE OK** (transcript: `evals/reports/smoke-transcript.txt`) |
| Web runtime | API :4100 + `next start` :3000 | `/`, `/login`, `/escalations`, `/admin` all **HTTP 200** |
| Seed | `npm run seed` | `{"articles":20,"customers":10,"orders":10,"inquiries":30}` |

Test breakdown (43): db store capability/tenant/checkpoint/idempotency 5 ·
retrieval 5 · japanese-quality 2 · modelrail-client contract 5 · redaction 3 ·
pipeline/integration/negative-controls 9 · approval+executor 6 ·
MCP v2 stdio integration 3 · API 5.

## Evaluation headline metrics (gold v1.0.0, mock LLM)

| Metric | Value | Denominator |
|---|---|---|
| Classification accuracy | 0.98 | 49/50 |
| Classification macro-F1 | 0.9753 | 9 intents |
| Escalation precision / recall / F1 | 1 / 1 / 1 | TP=16 FP=0 FN=0 |
| False-safe (must-escalate drafted) | 0 | 16 must-escalate |
| Retrieval Recall@5 | 0.9625 | 40 labelled cases |
| Retrieval MRR | 0.8688 | 40 labelled cases |
| Citation precision / recall | 0.6912 / 0.9412 | 34 drafted labelled cases |
| Verification pass rate (drafted) | 1.0 | 34 drafted |
| Forbidden-claim violations | 0 | 50 cases |
| Safety (agent writes / unapproved / duplicates / cross-tenant) | 0 / 0 / 0 / 0 | eval run |
| Latency p50 / p95 (in-process mock) | 4ms / 8ms | 50 runs — not a production figure |

Gates (SPEC §11.3): escalation recall ≥0.90 **PASS** (1.0) · intent accuracy
≥0.85 **PASS** (0.98) · macro-F1 ≥0.85 **PASS** (0.9753) · Recall@5 ≥0.90
**PASS** (0.9625) · hard zero-tolerance **PASS**.
Known misses, honestly: 1 classification miss (adversarial discount case
labelled `other`, classified `price_stock` — it still escalates correctly);
3 retrieval misses (a hiragana-typo case ranking, an FAQ-vs-manual ranking,
one mixed delivery+invoice query); citation precision is a strict lower
bound because gold labels are single-article while drafts legitimately cite
a sibling procedure article.

## SPEC §2.2 acceptance criteria — item by item

1. `docker compose up --build` starts PG/API/web/workers — **NOT VERIFIED**
   (no Docker here). Compose file, Dockerfiles, PG schema/roles provided.
2. Seed loads 30 inquiries / 20 KB articles / 10 customers+orders / 2 roles —
   **Verified** (seed output above; 2 tenants seeded for isolation).
3. Routine delivery inquiry completes pipeline, cites KB, finds order,
   reaches `awaiting_review` — **Verified** (pipeline test + live smoke).
4. Operator inspects evidence, edits draft, approves only selected actions —
   **Verified** (API revision endpoint creates a new immutable revision and
   re-verifies; partial-approval executor test; web Review UI built).
5. Approval persists exact revision + payloads; executor creates ticket +
   outbox; repeat creates no duplicates — **Verified** (executor tests +
   smoke: re-approval 409, counts stable at 1/1).
6. Complaint / no-evidence / identity-ambiguous / safety cases escalate
   without a customer draft — **Verified** (tests + eval: 16/16 escalations,
   complaint handoff summary asserted, no draft rows).
7. Prompt/tool-output injection cannot cause unapproved writes or change
   the action set — **Verified** (8/8 adversarial gold cases; agent store
   handle structurally rejects writes; ticket tool refuses unapproved IDs).
8. Edited draft is re-verified; old verification/approval not reusable —
   **Verified** (revision endpoint re-runs verifier; REVISION_MISMATCH and
   DRAFT_NOT_PENDING rejection tests).
9. Worker crash recovery: agent resumes from checkpoint without duplicating
   steps; executor crash/replay creates no duplicate action — **Verified**
   (crash-after-`retrieve` test with lease expiry; idempotent replay tests;
   ambiguous outcome → `unknown`, never blind-retried).
10. Tenant-scoped everything; cross-tenant fails closed — **Verified**
    (store tests, sakura-tenant pipeline test, eval safety zeros).
11. Every run creates a trace; Kairo down does not fail the workflow —
    **Verified locally** (OTel-shaped spans in the in-memory collector;
    exporter-down test passes). Live Kairo export **NOT VERIFIED**.
12. ModelRail failure → bounded visible failure — **Verified**
    (`GATEWAY_UNAVAILABLE` test with failing transport + timeout mapping;
    contract tests for chat/usage/errors). Live gateway **NOT VERIFIED**.
13. CI runs the suites — CI workflow provided (`.github/workflows/ci.yml`),
    **NOT RUN** (no push performed). The same commands were run locally.
14. 50-case evaluation report in JSON + Markdown with denominators and
    versions — **Verified** (`evals/reports/latest.json`, `latest.md`).
15. Public demo constraints — no public deployment was made; API refuses
    arbitrary submission when `NODE_ENV=production` without DEMO_MODE.
16. README documents actual state, limitations, credentials, measured
    results only — **Verified** (README "Status (honest)" section).

## SPEC §20 honest-release checklist

- [x] All stated components exist and are exercised by tests.
- [x] Agent worker has no write permissions and cannot invoke the executor
      (capability-scoped store handle + read-only MCP servers only; proven
      by tests that assert CapabilityError).
- [x] Approval bound to exact draft revision + selected action payloads
      (hashes verified at approval and again in the executor/ticket tool).
- [x] Leases, checkpoints, idempotency tested (incl. crash-resume and
      ambiguous-outcome `unknown`).
- [x] Japanese retrieval metrics computed from labelled cases.
- [x] Verifier limits documented (docs + code comments); no semantic-
      entailment claim anywhere.
- [x] Japanese content review status stated accurately:
      **not native-reviewed** (dataset README, reports, README).
- [x] ModelRail and Kairo failure modes tested (gateway-down, exporter-down).
- [x] Mock mode labelled as mock in API health, worker logs, eval reports,
      README, and this report.
- [x] No public deployment; no real external action possible.
- [x] Evaluation reports contain dataset/prompt/policy/model/index/judge
      versions.
- [x] README/docs contain measured values only (all from the eval run above).
- [x] AGENT-LOG.md records the actual agent (Muse), real corrections
      (embeddings spike, tokenizer choice, seed redaction bug, relevance
      gate rebuild, worker-hang fix), and verification commands.

## Deviations from spec (with reasons)

1. **Dense retrieval disabled.** ModelRail embeddings spike: the sibling
   ModelRail codebase (`~/workspace/modelrail`) contains no embeddings
   endpoint (verified by source search; chat/completions only). Per SPEC
   §8.3/§19.1 the fallback is lexical-only, recorded — never silently
   bypassed. Client `embeddings()` throws `EMBEDDINGS_UNSUPPORTED`.
   RRF helper present but unused.
2. **Tokenizer:** kuromoji not adopted. Explicitly selected dictionary-free
   tokenizer (SPEC §8.2 allows "another explicitly selected tokenizer"):
   NFKC + particle-split character bigrams + a small documented
   query-expansion dictionary for hiragana spellings. Retrieval is called
   **lexical retrieval**, never BM25.
3. **MCP v2 used, no v1 fallback needed:** `@modelcontextprotocol/server`
   2.4.0 + `@modelcontextprotocol/client` 2.4.0 stdio smoke test passed in
   this Node 24 runtime before adoption (echo tool listed and called), and
   the real KB/CRM servers are covered by subprocess integration tests.
4. **Runtime:** local processes run TypeScript via `tsx` instead of a
   compiled `dist/` build; Dockerfiles do the same (unexecuted).
5. **npm workspaces** are the working install path; `pnpm-workspace.yaml`
   is shipped per SPEC §13 but pnpm is not installed here (unverified).
6. **Classifier/policy calibration:** the deterministic mock classifier's
   keyword sets, the policy pattern lists, and the retrieval coverage
   thresholds (0.18 standard / 0.12 intent-matched) were calibrated against
   the labelled gold set, as SPEC §5.4/§8.3 prescribe. The overseas rule is
   evidence-driven (a scope term absent from all retrieved evidence has no
   grounded answer).

## NOT verified (explicit)

- PostgreSQL/Drizzle execution, pgvector, `tsvector` ranking, RLS, and the
  DB role grants in `pg-roles.sql` (provided, never run against real PG).
- Docker / Docker Compose startup and all Dockerfiles.
- Real ModelRail live inference (no gateway available; client contract-
  tested against mock transports only).
- Live Kairo OTLP ingestion (exporter implemented + failure-tested only).
- GitHub CI run (workflow provided; nothing pushed).
- Native Japanese review of fixtures/drafts (none available).
- pnpm workspace install.
- The seeded `data/benri.sqlite` in this workspace is a build artifact;
  a clean checkout reproduces it with `npm run seed`.

## Live smoke transcript (2026-11-12, MCP gateways on)

Full transcript committed at `evals/reports/smoke-transcript.txt`. Summary:
seed ok → API up → login ok → invoice run `awaiting_review` with actions
[send_demo_reply, create_ticket] → approval accepted → executor (ticket via
MCP) created exactly 1 ticket + 1 outbox → re-approval rejected (409) →
replay created no duplicates → complaint escalated
(COMPLAINT_REQUIRES_HUMAN) with NO draft → injection escalated
(POLICY_NOT_COVERED) with no new writes → **SMOKE OK**.
