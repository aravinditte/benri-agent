# Agent work log

## 2026-11-14 - Complete the v3 PostgreSQL and CI release path
Agent: Codex
Task: Implement the attached v3 build specification in the existing repository and prepare a directly pushable release.
Produced: Replaced the PostgreSQL stub with a working repository adapter and contract tests; added deterministic migration and seed ordering; verified the full Compose stack and a PostgreSQL-backed workflow; added frontend linting and component tests; upgraded pinned web dependencies; added GitHub Actions; and refreshed the verification evidence.
Accepted: Shared repository behavior between SQLite and PostgreSQL, least-privilege database roles, separate seed process, worker-safe leasing, generated-code drift checks, Linux race testing, and exact frontend dependency pins were retained because they satisfy the capability, reproducibility, and verification requirements.
Changed/rejected: Replaced the nonexistent `pressly/goose` container reference with the official `ghcr.io/pressly/goose` image; removed the PostgreSQL methods that returned an unverified-path error; and corrected Windows executable lookup assumptions in smoke and MCP subprocess tests.
Verification: `go vet ./...`; `go test ./... -count=1`; `go build ./cmd/...`; Linux `go test -race ./... -count=1`; PostgreSQL repository contract test; SQLite/MCP smoke test; evaluation gates; `npm run typecheck`; `npm run lint`; `npm test`; `npm run build`; `docker compose up --build -d`; API health check; web login check; and a PostgreSQL-backed end-to-end run through completion.
Commit: 1f5aede
