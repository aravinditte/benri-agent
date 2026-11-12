# AGENT-LOG — coding-agent session log (SPEC §11)

One entry per session, written the same day. Entries with corrections are the
valuable ones — do not delete or soften them.

<!--
## YYYY-MM-DD — <task>
Agent: Claude Code · Prompt summary: …
Produced: …
I changed / rejected: …
Verified by: …
-->

## 2026-11-12 — Full v2 build (Phases A–E in one delivery)
Agent: Muse (NOT Claude Code — this build ran in the Muse VM, and the log names the agent honestly)
Task: Implement Benri Agent Build Spec v2.0 end-to-end: monorepo, sqlite store + PG/Drizzle schema, agent/executor split, MCP v2 servers, pipeline, approval transaction, API, Next.js UI, fixtures, 50-case eval, docs.
Produced: Complete source in this repo; 43 vitest tests passing; eval report in evals/reports/; live process smoke transcript in evals/reports/smoke-transcript.txt.
Accepted: npm workspaces (pnpm-workspace.yaml also shipped per spec §13); node:sqlite portable store as the verified path with capability-scoped handles; MCP v2 (@modelcontextprotocol/server + client 2.4.0) after a stdio smoke test passed; lexical-only retrieval.
Changed/rejected (real corrections during this build):
- Embeddings spike: the sibling ModelRail codebase has NO embeddings endpoint (source search) → dense retrieval disabled and documented, not silently bypassed (SPEC §19.1).
- Tokenizer: kuromoji not adopted; explicitly selected dictionary-free tokenizer (NFKC + particle-split char bigrams) — retrieval is called lexical, never BM25.
- Seed redaction bug caught in testing: the seed CLI's inline postal-code regex mangled order numbers (HIN-2026-0001 → "HIN-2[郵便番号]"). Fixed by routing all seeding through the real redactor and adding an explicit order-number mask pattern that runs before the postal pattern.
- Retrieval relevance: naive bigram scoring ranked everything relevant (generic polite-phrasing bigrams). Rebuilt as IDF-weighted content-coverage with an intent-aware gate; thresholds (0.18 / 0.12) calibrated on the gold set per SPEC §8.3. Classifier/policy were likewise calibrated against the labelled set (SPEC §5.4): occurrence-weighted keywords, deterministic patterns for injection/credential-disclosure/out-of-scope （海外） topics, ambiguous multi-order detection.
- Worker `--once` hang: MCP client transports were never closed, so one-shot workers never exited (and an orphaned worker later interfered with a smoke re-run by holding a deleted DB inode). Fixed by closing gateways in once mode; smoke now kills the API process group.
- First smoke draft assumption was wrong (a delivery inquiry proposes only send_demo_reply, no ticket): smoke now drives an invoice inquiry for the ticket+outbox flow — the design was right, the test script was not.
Verification: `npx tsc --noEmit` clean; `npx vitest run` 43/43 (core suites run twice); `npm run eval` — accuracy 0.98 (49/50), macro-F1 0.9753, escalation P/R/F1 1/1/1 (16/16, 0 false-safe), Recall@5 0.9625 (40 labelled), safety zeros; web `tsc` + `next build` pass and /, /login, /escalations, /admin serve 200; `npx tsx scripts/smoke.ts` → SMOKE OK.
Commit: not committed/pushed by the agent — left for the owner's review.
