# Benri Agent

Human-in-the-loop AI customer-operations workbench for Japanese inquiries.
Benri turns Japanese customer inquiries into evidence-backed response drafts
and explicit action proposals. An authenticated operator reviews the exact
draft and each proposed action; only then can a separate executor write to
the demo fixture system. **The model can never execute writes.**

Spec: [SPEC.md](SPEC.md) (v2.0) · Architecture: [docs/architecture.md](docs/architecture.md) ·
Threat model: [docs/threat-model.md](docs/threat-model.md) ·
Eval methodology: [docs/eval-methodology.md](docs/eval-methodology.md) ·
Runbook: [docs/runbook.md](docs/runbook.md) · Verification: [VERIFICATION-REPORT.md](VERIFICATION-REPORT.md)

## Status (honest)

- **Locally verified** on the portable `node:sqlite` store with the
  deterministic mock LLM (`MOCK_LLM=1`): full pipeline, approval/executor
  boundary, MCP servers over stdio, API, web UI build + runtime, 43 tests,
  50-case evaluation, and a live process smoke — see VERIFICATION-REPORT.md.
- **Provided but NOT verified here** (no Docker/PostgreSQL on the build
  machine): the PostgreSQL/Drizzle path, `docker compose up`, Dockerfiles,
  and GitHub CI. They are labelled as such wherever they appear.
- **Not done / not claimed:** live ModelRail inference (client + contract
  tests exist; no live gateway was run), live Kairo export (OTLP exporter
  implemented; failure-tested only), dense retrieval (disabled — ModelRail
  exposes no embeddings endpoint; lexical retrieval only), native Japanese
  review of fixtures (**not native-reviewed**).

## Measured results (mock LLM, gold set v1.0.0)

From `evals/reports/latest.md` (denominators included there):

- Classification accuracy 0.98 (49/50), macro-F1 0.9753
- Escalation precision/recall/F1 = 1 / 1 / 1 (16/16 must-escalate caught, 0 false-safe)
- Retrieval Recall@5 0.9625, MRR 0.8688 (40 labelled cases)
- Citation precision 0.6912 / recall 0.9412 (strict single-article gold labels)
- Safety: agent writes 0, unapproved actions 0, duplicates 0, cross-tenant 0

These are deterministic-mock pipeline measurements, **not** live-model results.

## Quick start

```bash
npm install
npm run seed            # fixtures -> ./data/benri.sqlite
npm run api             # http://localhost:4100
npm run agent-worker    # add BENRI_USE_MCP=1 to run tools via MCP servers
npm run executor-worker
cd apps/web && npm install && npm run build && npm start   # http://localhost:3000
```

Windows PowerShell (no `&&`; one command per line):

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

Demo credentials (fixture tenant only, documented on purpose):
operator `operator@hinata.example` / `demo-operator-pass`;
admin `admin@hinata.example` / `demo-admin-pass`.

Demo walkthrough: open the queue, run INQ-006 (invoice), watch the run reach
`awaiting_review`, inspect evidence + verification, approve both actions,
then check Tickets and the Outbox — the reply is labelled
**"Sent in demo outbox — not delivered"** because nothing is ever really sent.
Open a complaint inquiry to see escalation with an internal handoff summary
and no customer draft.

## Layout

`apps/` api · web · agent-worker · executor-worker ·
`packages/` agent-core · retrieval · mcp-kb · mcp-crm · mcp-ticket · db ·
observability · modelrail-client · japanese-quality ·
`evals/` dataset (50 gold cases) · runner · judges · reports ·
`fixtures/` kb (20 articles) · crm (10 customers/orders) · inquiries (30)

## Limitations

- Fixture data is synthetic and fictional (Hinata Store); no real customer
  data, no real channels (no email/LINE), no real payments/refunds.
- The verifier checks citations, structured facts, numbers/dates and style
  mechanically; it does not prove semantic entailment. Human review is
  mandatory by design.
- Japanese content is **not native-reviewed**.
- PII redaction is a limited pattern detector for common Japanese formats.
