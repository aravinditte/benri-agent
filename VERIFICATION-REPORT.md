# Benri Agent — Verification Report (v3.0, Go + TypeScript)

Build: Build Spec v3.0 rebuild, 2026-11-12. Agent: Muse (see
AGENT-LOG.md). The v2 report is archived at
`docs/verification-report-v2.md`. Everything below was executed on this
machine unless it is listed under **NOT verified**.

## Toolchain (pinned)

- Go **1.27.4** (downloaded from go.dev, tarball sha256
  `7226736c2339719cda28fbf3a54d71aa63bcefe376e98cef00034268dab24840`),
  pinned in `go.mod`, CI, and `Dockerfile.backend`.
- oapi-codegen **v2.8.2**, sqlc **v1.31.1**, goose **v3.30.0**.
- Key Go deps (go.mod/go.sum): chi v5.5.0, pgx/v5 v5.13.0, MCP go-sdk
  **v1.8.0** (stdio spike: negotiated protocol version **2026-07-28**),
  kagome/v2 v2.11.1 + kagome-dict/ipa & uni v1.2.6, otel v1.47.0,
  modernc.org/sqlite v1.62.0 (pure Go, no cgo), x/text v0.44.0.
- Web (exact pins in web/package.json): next 15.5.29, react 19.1.0,
  typescript 5.9.3, openapi-typescript 7.13.0.

## Verification commands — real outputs

| Check | Result |
|---|---|
| `gofmt -l cmd internal` | clean (no files listed) |
| `go vet ./...` | clean |
| `go build ./...` | OK |
| `go test ./... -count=1` | **35/35 PASS, 0 fail** (8 packages) |
| `CGO_ENABLED=1 go test -race ./... -count=1` | **all 8 packages PASS** |
| `make generate-check` | `generate-check: no drift` (oapi-codegen + openapi-typescript + sqlc regenerated and diffed) |
| web `npm run typecheck` | clean |
| web `npm run build` | PASS |
| web runtime vs Go API | 200 on /, /escalations, /admin, /login, /inquiries/INQ-001 |
| `go run ./cmd/eval` (bin/eval) | exit 0, all §11.3 gates PASS (below) |
| `make smoke` (bin/smoke) | **SMOKE OK** — transcript at `evals/reports/smoke-transcript.txt` |

## Eval headline metrics (mock LLM, gold v1.0.0, Go runner)

| Metric | Value | Denominator |
|---|---|---|
| Classification accuracy | 0.9800 | 49/50 |
| Classification macro-F1 | 0.9753 | 9 intents |
| Escalation precision / recall / F1 | 1.0000 / 1.0000 / 1.0000 | TP=16 FP=0 FN=0 (16 must-escalate) |
| Retrieval Recall@5 | 0.9625 | 40 labelled cases |
| Retrieval MRR | 0.8875 | 40 labelled cases |
| Citation precision / recall | 0.7941 / 0.9265 | 34 drafted labelled cases |
| Verification pass rate | 1.0000 | 34 drafted |
| Forbidden-claim violations | 0 | 50 cases |
| Latency p50 / p95 (in-process mock) | ~354ms / ~422ms | 50 runs — NOT production figures |

Kagome benchmark (§8.2, labelled corpus, search mode): IPA Recall@5
**0.9375**, MRR **0.8958**; UniDic Recall@5 **0.9375**, MRR **0.8917**.
Winner: **IPAdic** (`kagome-ipa-1.2.6`), index version
`lexical-v3-kagome-ipa-1.2.6`. The v2 retrieval thresholds (content 0.18,
intent-gate 0.12) carried over; the Go eval reproduced v2's headline
metrics exactly (accuracy, macro-F1, escalation, Recall@5 identical;
citation precision/recall differ slightly — 0.7941/0.9265 vs v2
0.6912/0.9412 — because Kagome tokenization changes draft sentence
selection).

Dataset/fixtures byte-identical to v2 (sha256): gold.jsonl
`bb749920…e49`, inquiries `5df7ec08…229`, articles `cd665e58…152`,
customers_orders `e69ee8cc…ceb1`.

## Acceptance criteria (§2.2), item by item

1. **Compose stack** — `docker-compose.yml` + Dockerfiles provided with
   per-role env (§18). **UNEXECUTED here (no Docker)** — see NOT verified.
2. **Seed** — VERIFIED: Go seed loads 30 inquiries / 20 KB articles /
   10 orders + customers / 2 tenants / demo operator+admin users,
   redacted at seed with the real redactor (test + smoke).
3. **Routine pipeline** — VERIFIED: pipeline tests + eval (49/50 intent
   accuracy) + smoke; INQ-001/INQ-006 reach awaiting_review with
   citations and CRM order context.
4. **Inspect/edit/approve selected** — VERIFIED: draft detail API,
   revision endpoint (re-verifies), approval over HTTP test; web UI
   screens kept from v2.
5. **Approval binding + exactly-once** — VERIFIED: approval/executor
   flow tests (exactly 1 ticket + 1 outbox, replay-safe), re-approval
   refused, wrong revision/hash refused, partial approval honoured.
6. **Escalation classes** — VERIFIED: complaint + no-evidence pipeline
   tests; eval: 16/16 must-escalate caught, 0 false-safe. (Identity-
   ambiguous and safety-sensitive cases are in the gold set and pass.)
7. **Injection resistance** — VERIFIED: injection pipeline test + smoke
   (escalates, no writes, action set unchanged — actions are
   deterministic templates, never model output); tool-output injection
   hook (MalformedToolOutput) ported in the pipeline.
8. **Edited draft re-verified** — VERIFIED: revision endpoint rebuilds
   the verification context and re-runs the verifier; approval
   re-verifies again and refuses with REVERIFICATION_FAILED; approvals
   are bound to revision+hash so a prior approval cannot cover a new
   revision (tests: REVISION_MISMATCH / DRAFT_NOT_PENDING paths).
9. **Kill/resume** — VERIFIED for the agent (crash-after-step test:
   resume completes with no duplicated steps). Executor kill-during-write
   is covered by the ambiguous-outcome test (job → `unknown`, never
   blind-retried) and idempotent replay; a literal mid-write process kill
   was not performed.
10. **Tenant scoping** — VERIFIED: store cross-tenant test, pipeline
    cross-tenant test, MCP tenant-binding test (argument tenant ≠ launch
    tenant refused).
11. **Traces** — VERIFIED locally: trace-tree test asserts `benri.run` +
    step spans in the in-memory recorder with a deliberately unreachable
    OTLP endpoint (export failure does not fail the run). Live Kairo
    ingestion is NOT verified.
12. **ModelRail failure** — VERIFIED: gateway-down test → run `failed`
    with GATEWAY_UNAVAILABLE; per-run hard limits bound every run.
13. **CI test coverage** — CI yaml provided (unit/integration/state/
    authorization/injection/retrieval/eval suites are the Go tests +
    eval gate). **CI itself NOT run** (no GitHub run in this delivery).
14. **Eval report** — VERIFIED: `evals/reports/latest.json` + `latest.md`
    with denominators and dataset/prompt/policy/model/index versions.
15. **Public demo** — not deployed in this delivery; demo mode is
    fixture-only + mock-labelled by construction (`/health` mode label).
16. **README honesty** — README documents deployment state, limitations,
    demo credentials, and only the measured results above. No Node.js
    backend claim (§0.2): the backend is Go; TS is the web UI only.
17. **Go/web CI checks (#17, NEW)** — gofmt/vet/test/test-race all run
    locally (above) and are wired into `.github/workflows/ci.yml`. Web:
    typecheck + build wired and run locally. GAP: the web app has no
    eslint config or UI test suite (same as v2), so CI has no web
    lint/UI-test step — recorded here rather than faked.
18. **Generated contract types (#18, NEW)** — VERIFIED: Go server stubs +
    types from oapi-codegen, TS types from openapi-typescript, sqlc
    output — all generated from `api/openapi.yaml` / `db/queries`, all
    committed, `make generate` + drift check exercised locally (no
    drift).

## Trust boundary (§9, v3 finer) — test evidence

`internal/store` capability tests prove: agent handle cannot insert
approvals / execution jobs / tickets / outbox; api handle cannot write
tickets / outbox; executor handle cannot insert tickets directly (only
the ticket-mcp role can); kb-mcp handle cannot write. The agent-worker
image ships no mcp-ticket binary; the executor image ships only
mcp-ticket and its env carries no ModelRail key.

## Deviations from spec (and why)

1. **PgStore method bodies are explicit stubs** returning an
   "unverified" error. The PG path ships goose migrations (incl.
   per-role grants), sqlc queries + generated `dbgen` code (generation
   verified), the pgx pool handle, and per-role URL wiring — but with no
   PostgreSQL on this machine, a blind 70-method port could not be
   verified and would be a false claim. The SQLite store is the verified
   implementation of the same contract.
2. **ApproveRequest shape**: the canonical OpenAPI `ApproveRequest`
   carries `revision` + `selected_action_ids` only, so the API binds the
   stored payload hashes (the ones shown in the draft detail) into the
   approval record. The executor/ticket server still re-validate stored
   payload ↔ stored hash ↔ approval before any write.
3. **Japanese style length check** uses rune counts (v2 used JS UTF-16
   lengths) — a deliberate, recorded porting difference; thresholds are
   generous (80–1200) and no gold case is near the boundary.
4. **Smoke/Makefile `seed` recipe** seeds via API first-boot (the same
   code path used everywhere) rather than a separate seed binary.

## NOT verified

- PostgreSQL execution of any kind (migrations, grants, sqlc queries,
  PgStore) — no PostgreSQL on this machine.
- Docker / Docker Compose (Dockerfiles + compose provided, unexecuted).
- GitHub CI run of `.github/workflows/ci.yml` (provided, not run).
- Live ModelRail inference (mock mode verified; live-response parsing is
  contract-tested with an injected transport).
- Live Kairo/OTLP ingestion (span tree verified in-memory; exporter is
  async and failure-isolated).
- Native Japanese review of fixtures/gold text (labelled
  `not_native_reviewed` everywhere).
- pnpm (v3 uses npm workspaces for web only; the Go backend has no JS).
