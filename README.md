# Benri Agent (便りエージェント) — v3

An approval-gated AI support copilot for Japanese e-commerce support
teams. The agent classifies a customer inquiry, retrieves grounded
knowledge-base evidence, checks a deterministic policy layer, drafts a
Japanese reply, and **proposes** side-effecting actions. Nothing executes
without a human approving the exact draft revision and the exact action
payloads; a separate executor then performs only what was approved —
exactly once.

**Stack (v3): Go backend + TypeScript web UI.** The entire backend —
API, agent worker, executor worker, three MCP servers, eval CLI — is a
single Go module. TypeScript remains only in `web/` (Next.js operator
UI). _Honesty note (SPEC §0.2): this project's backend is Go; it is not
evidence of Node.js backend work. The earlier TypeScript backend (v2)
exists in git history and in `docs/verification-report-v2.md`._

## Why it exists

Support automation fails dangerously when a model can both draft and
act. Benri splits the two: the model can only draft and propose; humans
approve; a separate, dumber executor acts. Every step is checkpointed,
hashed, and auditable, and the whole flow is evaluated against a
versioned 50-case Japanese gold dataset.

## Measured results (this machine, Go build, mock LLM)

From `go run ./cmd/eval` — see `evals/reports/latest.md`:

| Metric | Value | Denominator |
|---|---|---|
| Classification accuracy | 0.98 | 49/50 |
| Classification macro-F1 | 0.9753 | 9 intents |
| Escalation precision / recall / F1 | 1.0 / 1.0 / 1.0 | TP=16 FP=0 FN=0 |
| Retrieval Recall@5 | 0.9625 | 40 labelled cases |
| Citation precision / recall | 0.7941 / 0.9265 | 34 drafted labelled cases |
| Verification pass rate | 1.0 | 34 drafted |
| Forbidden-claim violations | 0 | 50 cases |
| Safety zeros (agent writes / unapproved / duplicates / cross-tenant) | 0 / 0 / 0 / 0 | eval run |

Kagome dictionary benchmark (§8.2): IPA Recall@5 0.9375 / MRR 0.8958 vs
UniDic 0.9375 / 0.8917 (40 labelled) → **IPAdic chosen**,
index version `lexical-v3-kagome-ipa-1.2.6`.

Go tests: 35/35 pass (`go test ./...`), race detector clean
(`go test -race ./...`). Live smoke with MCP subprocess gateways:
**SMOKE OK** (`evals/reports/smoke-transcript.txt`).

These are deterministic **mock-LLM** numbers — they measure the mock
pipeline against the gold set, not a live model. Japanese fixtures are
synthetic and **not native-reviewed**.

## Quick start (local, verified path)

Requirements: Go 1.27.4, Node 22 (web only).

```bash
go build -o bin/ ./cmd/...
BENRI_DB_PATH=./data/benri.sqlite ./bin/api     # seeds demo data on first boot → :8080
./bin/agent-worker --once                        # process queued runs
./bin/executor-worker --once                     # execute approved jobs
cd web && npm install && npm run dev             # operator UI → :3000
```

Windows PowerShell (no `&&`):

```powershell
go build -o bin/ ./cmd/...
$env:BENRI_DB_PATH = "./data/benri.sqlite"
./bin/api
```

Demo users: `operator@hinata.example` / `demo-operator-pass` ·
`admin@hinata.example` / `demo-admin-pass` (local demo only).

Try the flow: log in → open inquiry INQ-006 (invoice request) → start a
run → `./bin/agent-worker --once` → review the draft, its citations and
the two proposed actions → approve → `./bin/executor-worker --once` →
exactly one ticket and one demo outbox message exist. Re-approving is
refused (409). A complaint (INQ-014) escalates to a human with a Japanese
handoff summary and no draft.

## MCP mode

Workers use the same tool logic in-process by default. Set
`BENRI_USE_MCP=1` to route through the real MCP server binaries over
stdio (official MCP Go SDK, negotiated protocol 2026-07-28):

```bash
export BENRI_USE_MCP=1
export BENRI_MCP_KB_BIN=./bin/mcp-kb BENRI_MCP_CRM_BIN=./bin/mcp-crm BENRI_MCP_TICKET_BIN=./bin/mcp-ticket
```

The agent worker can only reach the KB/CRM servers; only the executor
spawns the ticket server; tenant context is bound at server launch.

## Evaluation & smoke

```bash
make eval        # dictionary benchmark + 50-case gold run → evals/reports/
make smoke       # binaries + MCP end-to-end → SMOKE OK
make test        # go test ./...
make generate    # regenerate OpenAPI (Go/TS) + sqlc outputs
make generate-check   # drift check (fails on any generated-file drift)
```

## Layout

```
cmd/{api,agent-worker,executor-worker,mcp-kb,mcp-crm,mcp-ticket,eval,smoke}
internal/{api,auth,agent,approval,executor,jobs,modelrail,mcpclient,
          mcpserver,retrieval,verify,japanese,observability,config,
          domain,store,seed,proc,testutil}
api/openapi.yaml            canonical HTTP contract
db/{migrations,queries}     goose migrations (+ role grants), sqlc queries
web/                        Next.js operator UI (generated API types)
fixtures/ evals/            synthetic demo data + versioned gold dataset
docs/                       architecture, threat model, eval methodology, runbook
```

## PostgreSQL / Docker (provided, unverified here)

`docker-compose.yml` + `Dockerfile.backend` / `Dockerfile.web` run the
PostgreSQL path with per-role database users; goose migrations and sqlc
queries are in `db/`. The agent-worker image ships no ticket binary; the
executor image ships no ModelRail key. **No Docker or PostgreSQL exists
on the build machine, so this path is provided but unverified** — the
locally verified store is SQLite (pure-Go driver). Details and the full
item-by-item status: `VERIFICATION-REPORT.md`.

## Docs

- `docs/architecture.md` — processes, trust boundary, data path
- `docs/threat-model.md` — threats, controls, known limits
- `docs/eval-methodology.md` — dataset, metrics, gates, negative controls
- `docs/runbook.md` — operations and failure handling
- `SPEC.md` — the build specification (v3.0)
