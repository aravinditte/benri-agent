# Benri Agent — Week 0 kickoff pack

Prepared 2026-11-12 by Muse (research, not a build step). Spec: `SPEC.md` §14.
Status: scaffold only. No app code written, no KB articles, no eval cases —
those are Wizz's to write (SPEC §7, §10). This pack only removes the
"how do I start?" friction for whichever fork he picks.

## 1. The fork (already asked in the side chat — do not re-ask, just decide)

| Route | What it proves for Cogent | Cost / catch |
| --- | --- | --- |
| **A. Claude Code locally (recommended, SPEC §11)** | The real evidence: Claude Code as primary driver, `AGENT-LOG.md` with real corrections, from session 1 | Needs a paid Claude plan (Pro / Max / Team / Enterprise — Claude Code sign-in requires one). Needs Docker Desktop + Node/pnpm on his Windows machine. |
| **B. Build here with Muse** | Faster start, same spec, same verification pattern as Kairo/ModelRail | Evidence becomes Muse-based, not Claude Code — weaker for Cogent's #1 requirement. The Cogent pack already has a candidate Muse-based paragraph for this case; it does not become a Claude Code claim. |

Either route uses the same first-session prompt below. The only thing that
changes is where it is pasted.

## 2. Prerequisites checklist (check before session 1)

- [ ] **Claude Code (route A only)** — native Windows, no WSL, no Node needed.
  PowerShell (not as admin):
  ```powershell
  irm https://claude.ai/install.ps1 | iex
  claude --version
  claude   # first run opens the browser to sign in with a Claude account
  ```
  Alternative: `winget install Anthropic.ClaudeCode` (does not auto-update;
  the .ps1 installer does). Optional but recommended: Git for Windows, so
  Claude uses Git Bash instead of PowerShell for shell commands.
  Verified 2026-11-12 via Anthropic install docs as summarised in current
  Windows guides — re-check https://claude.ai/install.ps1 if it fails.
  Unknown: whether Wizz already has a paid Claude plan. If not, route A
  has a subscription cost; route B has none.
- [ ] **Docker Desktop (both routes, for local dev)** — Benri's compose file
  needs it for Postgres + pgvector. Image is the official
  `pgvector/pgvector:pg16` (`docker pull pgvector/pgvector:pg16`).
  This VM has no Docker, so nothing here can be verified locally — the
  first `docker compose up` is Wizz's machine (or CI), and only then may
  any Postgres/Docker claim be made.
- [ ] **Node 20+ and pnpm** — for the TypeScript workspace. `corepack enable`
  then `corepack prepare pnpm@latest --activate` is the usual path; pin the
  pnpm version in `package.json` (`packageManager` field) in session 1.
- [ ] **GitHub repo** — create empty `aravinditte/benri-agent` (public, like
  kairo/modelrail). Not created yet; do not push the scaffold until the
  first session lands something reviewable. Push pattern from Kairo/ModelRail:
  branch + PR, verify by blob SHA, `.github/workflows/ci.yml` may need
  manual adding (same 403 Workflows limit as before).
- [ ] **ModelRail + Kairo URLs** — not needed in Week 0. Both are external
  env config in Week 2; mock LLM is the dev default (SPEC §9, §14).

## 3. First-session prompt (paste as-is, route A or B)

> Work in ~/workspace/benri-agent (SPEC.md is the source of truth, CLAUDE.md
> carries the repo rules). Week 0 only — do NOT write any agent loop, MCP
> server, API route, UI page, KB article, or eval case yet.
>
> Task: make the monorepo runnable as an empty shell.
> 1. pnpm workspaces: root package.json + `apps/api`, `apps/worker`,
>    `apps/web`, `packages/agent-core`, `packages/mcp-servers`,
>    `packages/db` — each with a minimal package.json + tsconfig, no
>    application code, `pnpm install` clean.
> 2. `docker-compose.yml`: one service, `postgres` from
>    `pgvector/pgvector:pg16`, named volume, healthcheck, `.env.example`
>    with `DATABASE_URL` only. Prove it: `docker compose up -d` then
>    `CREATE EXTENSION IF NOT EXISTS vector;` succeeds and
>    `SELECT * FROM pg_extension WHERE extname='vector';` returns a row.
>    Record the exact commands + output in your reply.
> 3. `.gitignore` (node_modules, dist, .env, coverage), root README status
>    line stays honest: scaffold only, nothing verified yet.
> 4. No new dependencies without asking. Small diff, one commit-sized
>    change set.
>
> After the session: remind me to add the AGENT-LOG.md entry the same day,
> including anything I corrected or rejected.

## 4. Second-session prompt (outline only — run after session 1 passes)

Drizzle schema for the §8 tables (no seed content yet) + `drizzle-kit`
migrations that apply cleanly to the pgvector Postgres from session 1,
plus empty `fixtures/` JSON skeletons (`inquiries.json`,
`kb-articles.json`, `customers.json`) with the exact field names from
SPEC §7–§8 and 2 clearly-marked EXAMPLE rows each — Wizz replaces the
examples with his own 30 / 20 / 10 fixtures. No invented policies,
prices, or dates anywhere: example rows must be labelled EXAMPLE.

## 5. Guardrails for every session (from SPEC §0, §11)

- He reviews every diff before commit; commits are task-sized.
- The agent never invents a KB policy, price, or date — fixture facts
  live in `fixtures/` only, and he authors them.
- The 50 gold eval cases are his authorship, not the agent's (§10).
- `AGENT-LOG.md`: one entry per session, same day, minimum 10 entries
  with ≥3 substantive corrections before v1 is done. A log with zero
  corrections is not credible — keep the corrections in.
- No number goes in the README/CV/Zenn post unless measured by the eval
  harness or a load script in the repo (`[MEASURED]` placeholders only).
- The Cogent [ADD] paragraph is written FROM the log at the end — never
  before, never drafted in advance.

## 6. What is still unknown / for Wizz only

- Route A or B (the fork).
- Paid Claude plan: yes / no?
- Docker Desktop installed on his Windows machine? Node/pnpm versions?
- GitHub repo `aravinditte/benri-agent` created yet? (No record of it.)
- Live LLM provider/budget for real-model runs (Week 2, not Week 0) and
  deploy host (Render vs Fly.io, Week 3) — deliberately not decided here.
