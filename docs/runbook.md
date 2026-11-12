# Benri Agent — Runbook

## Local run (verified path: sqlite + mock LLM)

```bash
npm install
npm run seed          # loads fixtures into ./data/benri.sqlite
npm run api           # Fastify API on :4100 (seeds if empty)
npm run agent-worker  # polls for runs (BENRI_USE_MCP=1 to use MCP servers)
npm run executor-worker
cd apps/web && npm install && npm run build && npm start   # UI on :3000
```

Windows PowerShell (no `&&` — run each line separately):

```powershell
npm install
npm run seed
npm run api
npm run agent-worker
npm run executor-worker
cd apps\web
npm install
npm run build
npm start
```

Demo users (documented demo credentials, fixture tenant only):
`operator@hinata.example` / `demo-operator-pass` (operator),
`admin@hinata.example` / `demo-admin-pass` (admin).

One-shot workers for scripts/CI: append `--once`
(`npx tsx apps/agent-worker/src/main.ts --once`).

## Verification commands

```bash
npx tsc --noEmit -p tsconfig.json   # typecheck
npx vitest run                      # all suites
npm run eval                        # gold-set evaluation -> evals/reports/
npx tsx scripts/smoke.ts            # live process smoke (API+workers, MCP on)
```

## Failure handling

- Run stuck `running`: its lease expires (60s default); the next claim
  resumes from the last completed checkpoint. Completed steps are not
  repeated (checkpoint uniqueness on `(run_id, step_key, attempt)`).
- Execution job `unknown`: a fixture write may or may not have happened.
  **Do not retry blindly.** Reconcile by checking `tickets` /
  `outbox_messages` for the job's `source_action_id`, then resolve manually.
- `GATEWAY_UNAVAILABLE`: ModelRail unreachable or mock misconfigured. The
  run is marked failed with that code; an operator may start a new run.
- Kairo/OTLP down: spans are still collected locally; runs are unaffected.

## Resetting demo state

Stop the processes, delete `./data/benri.sqlite*`, re-run `npm run seed`.

## Configuration

See `.env.example`. Defaults: `MOCK_LLM=1`, `DEMO_MODE=1`,
`BENRI_DB_PATH=./data/benri.sqlite`, limits per SPEC §18
(`BENRI_MAX_MODEL_CALLS_PER_RUN=4`, `BENRI_MAX_TOOL_CALLS_PER_RUN=8`,
`BENRI_MAX_RUN_DURATION_MS=30000`).
