# Benri Agent — runbook (v3)

## Local run (verified path: SQLite + mock LLM)

```bash
go build -o bin/ ./cmd/...
BENRI_DB_PATH=./data/benri.sqlite ./bin/api            # seeds on first boot, :8080
./bin/agent-worker --once                              # process queued runs
./bin/executor-worker --once                           # execute approved jobs
cd web && npm run dev                                  # operator UI on :3000
```

Demo users (local only): `operator@hinata.example` / `demo-operator-pass`,
`admin@hinata.example` / `demo-admin-pass`.

Windows PowerShell (no `&&`):

```powershell
go build -o bin/ ./cmd/...
$env:BENRI_DB_PATH = "./data/benri.sqlite"
./bin/api
```

Set `BENRI_USE_MCP=1` (and `BENRI_MCP_KB_BIN` / `BENRI_MCP_CRM_BIN` /
`BENRI_MCP_TICKET_BIN`) to route tool calls through the real MCP server
binaries over stdio; otherwise workers use the same tool logic in-process.

## Live ModelRail mode

Set `MODELRAIL_BASE_URL` (+ `MODELRAIL_API_KEY`, `MODELRAIL_CHAT_MODEL`)
and `MOCK_LLM=0`. `/health` then reports `mode: live`. Note: ModelRail has
no embeddings endpoint — retrieval stays lexical in both modes.

## Evaluation

```bash
make eval          # dictionary benchmark + gold run; writes evals/reports/
make eval-report   # print the latest report
./bin/eval -benchmark-only
```

## Smoke

```bash
make smoke         # full binary + MCP end-to-end; ends with SMOKE OK
```

## Failure handling

- **Run `failed` with GATEWAY_UNAVAILABLE** — the model gateway is down or
  erroring. Check ModelRail health; runs are safe to re-request (run
  creation is idempotent per inquiry + request key).
- **Run stuck `running`** — the worker died mid-run. Leases expire after
  2 minutes; a later worker resumes from checkpoints (completed steps are
  not repeated). Do not edit run rows by hand.
- **Job `unknown`** — a tool outcome was ambiguous (e.g. the ticket
  request may have committed before a transport failure). NEVER requeue
  blindly: check Tickets for a record with the job's source action id,
  then reconcile manually.
- **Approval refused: REVISION_MISMATCH / ACTION_HASH_MISMATCH** — the
  draft or action changed after it was displayed. Reload the draft, review
  the current revision, approve again.
- **Approval refused: REVERIFICATION_FAILED** — the draft no longer passes
  the deterministic verifier against its evidence (e.g. KB article edited
  since). Create a new revision or reject.
- **Escalations queue growing** — read the handoff summaries
  (`/escalations`); they carry the reason code, the checked evidence, and
  the redacted inquiry summary. COMPLAINT_REQUIRES_HUMAN always needs a
  human reply outside the draft flow.

## PostgreSQL / Compose (unverified on the build machine)

`docker compose up --build` brings up PostgreSQL, goose migrations
(including per-role grants), the three backend services with per-role
database URLs, and the web UI. Set real passwords via your secret manager
— the compose file's `benri-dev-only` values are local placeholders.
