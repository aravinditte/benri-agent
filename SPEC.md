# Benri Agent — Revised Build Specification

**Version:** 2.0  
**Revision date:** 2026-11-12  
**Owner:** Itte Aravind  
**Repository:** `benri-agent`  
**Demo tenant:** Hinata Store (fictional Japanese home-goods retailer)  
**Product category:** Human-in-the-loop AI customer-operations workbench

> **One-line product definition:** Benri turns Japanese customer inquiries into evidence-backed response drafts and explicit action proposals. An authenticated operator reviews the exact content and actions before a separate executor can write anything to the demo system.

This document replaces v1. It is a buildable engineering specification, not a claim that the system has already been implemented or deployed. All performance, quality, cost, and reliability numbers must be measured before publication.

---

## 0. Executive recommendation

**Keep the project and the TypeScript/Node direction. Rebuild the trust boundary and reduce the v1 scope.** The product idea is credible, the Japanese workflow gives it a concrete domain, and the integration story across ModelRail and Kairo is a useful portfolio advantage. The strongest part is not the chatbot: it is the deterministic agent runtime, constrained retrieval, explicit human approval, recoverable execution, and evaluation system.

The v1 document had several design problems that should be corrected before implementation:

| V1 issue | V2 decision |
|---|---|
| The agent worker and execution path were not separated strongly enough. A token check alone is not a sufficient security boundary. | Separate the read-only agent worker from the privileged executor. Approval creates a transactionally persisted, payload-specific action record. |
| Complaint handling conflicted: some sections said draft then escalate; acceptance criteria required no draft. | In v1, complaints are human-led. Produce an internal handoff summary, not a customer-facing draft. |
| The spec called PostgreSQL `tsvector` search “BM25”. | Call it lexical full-text retrieval. Use `ts_rank`/`ts_rank_cd` plus dense retrieval and rank fusion. Do not call it BM25 unless BM25 scoring is actually implemented. |
| “Every factual claim is verified” overstated what deterministic checks can prove. | Deterministically verify citations, structured facts, dates, numbers, and policy-sensitive fields. State clearly that this does not prove all semantic entailment. Human review remains mandatory. |
| A `です・ます` linter was treated as a guarantee of natural business Japanese. | Mechanical style checks catch obvious violations; human/Japanese-quality review and a calibrated judge assess naturalness and appropriateness. |
| The schema suggested PII could be redacted before storage while also retaining the original body. | Persist only a redacted body for normal operation. No real customer data or real email integration in v1. Fixture identifiers are synthetic. |
| Worker restart recovery was described as re-queueing stale jobs, which can duplicate execution. | Use leases, checkpoints, idempotency keys, and explicit `unknown` outcomes for uncertain side effects. Never blindly retry an ambiguous write. |
| The embedding dimension and embedding path were assumed. ModelRail’s chat-completions support does not automatically imply an embeddings endpoint. | Verify the ModelRail embeddings contract before enabling dense retrieval. Keep the embedding provider explicit and testable. |
| The three-week part-time plan attempted product, MCP, security, evals, deployment, and launch content at once. | Deliver in capability gates. Treat public deployment and launch writing as later milestones, not as prerequisites for a functioning core. |
| The project was positioned as direct evidence of a coding-agent harness. | Be precise: Benri demonstrates an application-agent runtime and evaluation engineering, plus serious Claude Code usage. It does not, by itself, implement a general coding-agent platform. |

### Product and portfolio thesis

Benri should tell one coherent story:

1. A real operational problem with a narrowly defined workflow.
2. A bounded agent runtime that can reason and retrieve but cannot perform writes.
3. A separate, audited approval-and-execution path.
4. Japanese-language retrieval and response evaluation.
5. Measured behavior under normal, ambiguous, adversarial, and degraded conditions.
6. Production-oriented operations through ModelRail and Kairo.

Do not turn v1 into a multi-channel customer-service platform. The quality of the harness and evidence is more valuable than adding email, LINE, payments, or multiple agents.

---

## 1. Target users, use case, and scope

### 1.1 Users

- **Support operator:** reviews inquiries, evidence, drafts, and proposed actions; edits, approves, or rejects.
- **Administrator:** manages the demo knowledge base, views evaluation runs, and inspects audit events and traces.

The initial product uses two seeded demo roles. It is not a multi-customer SaaS product in v1.

### 1.2 Supported inquiry categories

Use a fixed taxonomy:

| Key | Japanese label | V1 handling |
|---|---|---|
| `delivery_status` | 配送状況 | Retrieve shipping policy and fixture order status; draft when evidence is sufficient. |
| `return_exchange` | 返品・交換 | Draft only when the applicable return policy and order context are available. |
| `invoice_request` | 請求書 | Explain the documented invoice process; propose a ticket if operator action is needed. |
| `product_usage` | 使い方 | Answer from product manuals/FAQs. |
| `cancellation` | 解約・キャンセル | Draft only when the policy and order state support the requested action. Never cancel an order. |
| `price_stock` | 価格・在庫 | Use only current fixture catalogue data. Do not infer stock or future prices. |
| `complaint` | クレーム | Always route to a human in v1; create an internal summary, no customer-facing reply draft. |
| `account` | アカウント | Escalate identity-sensitive or account-changing requests. General FAQs may be answered. |
| `other` | その他 | Escalate when no safe, relevant evidence exists. |

### 1.3 Supported workflow

1. Operator selects a seeded inquiry or submits a local development fixture.
2. API normalizes and redacts the content before it enters the agent context or normal application logs.
3. Agent classifies the inquiry and retrieves relevant knowledge-base evidence.
4. Agent performs a read-only lookup against synthetic CRM/order fixtures when an order identifier is available.
5. A deterministic policy layer decides whether the case is draftable or must be escalated.
6. The model drafts a 丁寧語 response using the evidence bundle and returns structured citation metadata.
7. Verifier checks schema, references, policy-sensitive facts, dates, numbers, and mechanical style constraints.
8. The run stops at `awaiting_review`. It has no write credentials and no route to the executor.
9. Operator reviews the exact draft and individually selects the proposed fixture actions to approve.
10. API persists the approved draft revision and exact action payloads in one database transaction.
11. A separate executor processes those approved actions idempotently and writes only to the fixture ticket/outbox tables.
12. Audit events and OpenTelemetry spans make the whole path inspectable.

**Important wording:** in the demo, “send reply” means “write a message to the simulated outbox”. It does not deliver an email or contact a real customer. The UI must label this action `Sent in demo outbox — not delivered`.

### 1.4 Non-goals for v1

- No Gmail, IMAP, LINE, WhatsApp, or other live channel integration.
- No real customer data, production PII, or real CRM integration.
- No real refund, cancellation, payment, inventory, or shipping mutation.
- No automatic reply or unattended write operation.
- No multi-agent orchestration. One bounded workflow is enough.
- No fine-tuning.
- No attempt to build a general-purpose workflow builder.
- No claim that a language linter proves Japanese fluency or business correctness.
- No multi-tenant SaaS billing or self-service tenant onboarding.

---

## 2. Product requirements and definition of done

### 2.1 Product goals

- **P1 — Useful workflow:** a seeded Japanese inquiry can be processed and reviewed in a short, repeatable demo.
- **P2 — Safe agent runtime:** the model cannot execute writes, change permissions, or bypass the approval boundary.
- **P3 — Evidence-grounded output:** every draft exposes its source articles and verification results; unsupported cases are escalated.
- **P4 — Recoverable backend:** worker crashes do not silently lose work or duplicate fixture actions.
- **P5 — Measured quality:** a versioned gold set and regression tests evaluate classification, retrieval, escalation, citations, and action safety.
- **P6 — Observable operation:** each run has a trace, step timeline, usage/cost record when available, and audit trail.
- **P7 — Honest portfolio evidence:** README and CV claims are tied to reproducible tests and measured results.

### 2.2 Release acceptance criteria

A v1 release is complete when all required items below are demonstrated from a clean checkout:

1. `docker compose up --build` starts PostgreSQL, API, web, agent worker, and executor with the documented local configuration.
2. Seed loads 30 synthetic inquiries, 20 Japanese KB articles, 10 synthetic customers/orders, and the two demo roles.
3. A routine delivery inquiry completes the pipeline, cites KB articles, finds the relevant fixture order, and reaches `awaiting_review`.
4. Operator can inspect the source evidence, edit the draft, and approve only the selected actions.
5. Approval persists the exact draft revision and action payloads; executor creates a fixture ticket and outbox record. Repeating the same request does not create duplicates.
6. Complaint, no-evidence, identity-ambiguous, and safety-sensitive cases escalate without a customer-facing draft.
7. Prompt-injection and tool-output-injection tests cannot cause an unapproved write or change the allowed action set.
8. An edited draft is re-verified. The previous verification and approval cannot be reused for a new revision.
9. Killing the agent worker mid-run resumes from the last completed checkpoint without duplicating completed steps. Killing the executor during a fixture write does not create a duplicate action.
10. Every business-data query is tenant-scoped. Cross-tenant access tests fail closed.
11. Every run creates a trace when Kairo is configured; Kairo being unavailable does not fail the business workflow.
12. ModelRail failure produces a bounded, visible failure state. A run never hangs indefinitely.
13. CI runs unit, integration, state-machine, authorization, injection, retrieval, and mock-evaluation tests.
14. A 50-case evaluation report is generated in JSON and Markdown. The report includes denominators and model/prompt/index versions.
15. Public demo, if deployed, is fixture-only and mock-LLM-only. It does not accept arbitrary public submissions or invoke paid model APIs.
16. README documents the actual deployment state, limitations, demo credentials/policy, and measured results. No unmeasured metrics appear in marketing or CV copy.

---

## 3. Architecture

### 3.1 Technology choices

- **Language/runtime:** TypeScript on a pinned Node.js LTS release.
- **API:** Fastify.
- **Web:** Next.js.
- **Database:** PostgreSQL; `pgvector` for dense retrieval when the embedding contract is verified.
- **ORM/migrations:** Drizzle ORM + Drizzle Kit.
- **MCP:** official MCP TypeScript SDK. At the 2026-11-12 revision date, use the v2 split packages (`@modelcontextprotocol/server` and `@modelcontextprotocol/client`) after a minimal transport/interop smoke test; do not accidentally copy v1 imports from older examples.
- **Validation:** Zod v4 or another Standard Schema-compatible validator, consistently applied at API, worker, and tool boundaries.
- **Telemetry:** OpenTelemetry JS, OTLP/HTTP exporter to Kairo.
- **Inference:** ModelRail OpenAI-compatible API for chat/completion calls.
- **Tests:** Vitest, PostgreSQL integration tests, and deterministic mock-model fixtures.
- **Local runtime:** Docker Compose.

Do not add Redis, Kafka, a separate queue product, or a workflow engine in v1. PostgreSQL-backed jobs are adequate at this workload, provided leasing and idempotency are implemented correctly.

### 3.2 Component diagram

```text
                         benri-agent
┌──────────────────────────────────────────────────────────────────────┐
│ Browser                                                              │
│   │                                                                  │
│   ▼                                                                  │
│ Next.js UI ───────────────► Fastify API                              │
│ Queue / review / audit       Auth, tenant scope, approval endpoint   │
│                             │                    │                   │
│                             ▼                    ▼                   │
│                     PostgreSQL             Approval transaction    │
│              inquiries / KB / runs          approvals / proposed_actions │
│              steps / drafts / audit         execution_jobs           │
│                       ▲                           │                  │
│                       │                           ▼                  │
│                Agent worker                 Executor worker           │
│                read-only role               privileged role           │
│                  │      │                         │                   │
│                  ▼      ▼                         ▼                   │
│               KB MCP  CRM MCP                 Ticket MCP              │
│               read    read                    write-only              │
│                  │                              │                    │
│                  └────► ModelRail                └──► fixture DB       │
│                         chat/completions                             │
│                                                                      │
│  Agent/API/Executor ── OpenTelemetry ──► Kairo (optional external)   │
└──────────────────────────────────────────────────────────────────────┘
```

### 3.3 Process and permission boundaries

- `api`: authenticates users, authorizes tenant-scoped requests, owns draft approval, and writes approved action records. It does not run model reasoning.
- `agent-worker`: claims agent runs, performs classification/retrieval/drafting/verification, and persists checkpoints. Its database role cannot write to `tickets` or `outbox_messages`. It starts only read-only KB and CRM MCP servers.
- `executor-worker`: claims approved action jobs and invokes the write-only ticket MCP server. It cannot call the LLM and does not accept instructions or tool arguments from a model.
- MCP servers are small domain packages launched locally over stdio by the authorized host process in v1. Do not expose them as unauthenticated network services. If a later deployment requires Streamable HTTP, add authentication, origin/host validation, and an explicit service-to-service trust model first.
- PostgreSQL is the source of truth for state, checkpoints, approvals, idempotency, and audit records.

The key invariant is structural: **the process that interprets untrusted customer text has no write capability.** A prompt rule is not a security boundary.

### 3.4 Job claiming and recovery

Use PostgreSQL row locking with `FOR UPDATE SKIP LOCKED` for both agent runs and execution jobs.

Each claimed job has:

- `lease_owner`
- `lease_expires_at`
- `heartbeat_at`
- `attempt_count`
- `last_error_code`
- stable `idempotency_key`

A worker renews its lease during long operations. Expired jobs may be reclaimed only according to their job type:

- **Agent run:** resume from the last successful checkpoint. A step is committed with a stable `(run_id, step_key)` key so completed steps are not repeated unnecessarily.
- **Fixture action:** execute in one database transaction with a unique idempotency key. If the outcome cannot be determined, mark it `unknown` and require operator reconciliation; do not blindly retry.

Do not implement “requeue every stale running row on boot” without leases and action-specific retry semantics.

---

## 4. State model and workflow transitions

Do not overload one `status` field to represent inquiry, run, draft, and execution state. They are separate lifecycles.

### 4.1 Inquiry state

`new → queued → processing → needs_review → closed`

Alternative terminal/exception states: `escalated`, `failed`, `cancelled`.

### 4.2 Run state

`queued → running → completed`

Alternative states: `escalated`, `failed`, `cancelled`.

`completed` means the agent produced a verified draft and stopped. It does **not** mean a customer response was sent.

### 4.3 Draft state

`pending_review → approved | rejected | superseded`

Every edit creates a new immutable draft revision. A revision has its own content hash and verification record. An approval can refer to only one exact revision.

### 4.4 Action state

`proposed → approved → queued → running → succeeded`

Alternative states: `rejected`, `failed`, `unknown`, `cancelled`.

An action is approved only after an authenticated human explicitly approves that exact action payload. Draft approval and action approval are related but distinct decisions.

### 4.5 Escalation reasons

Use stable machine-readable codes, for example:

- `COMPLAINT_REQUIRES_HUMAN`
- `NO_RELEVANT_KB_EVIDENCE`
- `LOW_CLASSIFICATION_CONFIDENCE`
- `ORDER_ID_AMBIGUOUS`
- `POLICY_NOT_COVERED`
- `IDENTITY_SENSITIVE_REQUEST`
- `LEGAL_OR_SAFETY_SENSITIVE`
- `VERIFICATION_FAILED`
- `MODEL_BUDGET_EXCEEDED`

The operator sees both the code and a concise Japanese explanation.

---

## 5. Agent runtime and deterministic control plane

The LLM proposes structured content. The application owns sequencing, validation, permissions, retries, budgets, and state transitions.

### 5.1 Pipeline

| Step | Operation | Owner | Failure behavior |
|---|---|---|---|
| 0 | Normalize Unicode (NFKC), enforce input size/type, redact supported PII patterns. | Deterministic code | Reject invalid input; never send unredacted input to the model. |
| 1 | Classify intent and urgency using the fixed taxonomy. | LLM + schema validator | Invalid enum/schema → one bounded retry, then fail/escalate. |
| 2 | Retrieve Japanese KB evidence. | Retrieval service / read-only MCP | No relevant evidence → escalate, no draft. |
| 3 | Look up fixture customer/order by a validated order identifier when present. | Read-only CRM MCP | Missing or ambiguous match → continue only if the answer does not require identity/order facts; otherwise escalate. |
| 4 | Apply escalation and policy rules. | Deterministic policy engine | Escalate before draft when any hard rule matches. |
| 5 | Draft response with article citations and structured factual references. | LLM | Schema failure or unsupported content → one repair attempt, then escalate. |
| 6 | Verify draft. | Deterministic verifier + optional quality judge | Hard-check failure → one repair attempt; second failure → escalate. |
| 7 | Persist draft, evidence references, verification result, and checkpoint. | Agent worker | Save state transactionally; finish at `awaiting_review`. |

There is no model-driven `execute` step in the agent loop. Proposed actions are generated from deterministic intent/policy templates, not free-form tool calls from the LLM.

### 5.2 Checkpoint and resume contract

Persist a checkpoint after every completed step. A checkpoint contains:

- run ID and step key
- step status and attempt number
- input/output references or redacted JSON
- prompt/model/config version where relevant
- start/end timestamps and duration
- trace/span identifiers
- failure code when applicable

Never persist secrets, access tokens, raw PII, or full unredacted customer messages in step payloads. On restart, the worker resumes from the earliest incomplete step using the saved, redacted state. A checkpoint is immutable after the next step begins; corrections are recorded as new attempts.

### 5.3 Hard limits

All limits are configuration, not magic constants hidden in prompts:

- maximum model calls per run
- maximum MCP calls per run
- maximum wall-clock duration
- maximum input and evidence token budget
- maximum output tokens
- per-run cost ceiling when ModelRail provides reliable usage/cost data
- one draft repair attempt
- bounded retry count for transient network errors

Use explicit error codes such as `STEP_LIMIT_EXCEEDED`, `RUN_TIMEOUT`, `TOKEN_BUDGET_EXCEEDED`, and `GATEWAY_UNAVAILABLE`. Do not publish placeholder thresholds as measured operating characteristics.

### 5.4 Classification confidence

Do not trust an LLM’s self-reported confidence as a calibrated probability. Treat it as a routing signal only. Thresholds must be selected against the labelled evaluation set and reported with their observed precision/recall. Escalation policy must also use deterministic signals such as unsupported intent, missing evidence, identity ambiguity, and safety-sensitive categories.

---

## 6. Approval, execution, and security model

This is the most important redesign in v2.

### 6.1 Approval is bound to exact content and exact actions

When the operator approves a draft, the API must:

1. Authenticate the operator and verify role/tenant access.
2. Load the exact draft revision and its latest verification record.
3. Re-run verification if the draft has changed since verification.
4. Display and require explicit selection of each proposed action.
5. Construct a canonical action payload containing tenant, inquiry, draft revision, action type, target, and arguments.
6. Compute and persist a content hash for each selected action.
7. In a single database transaction, persist the approval event and enqueue the selected action jobs.
8. Record operator ID, timestamp, draft hash, action hashes, and policy version in the audit log.

A previous approval is invalid for any changed draft revision, action payload, target, or tenant. The browser does not mint approval tokens.

### 6.2 Executor rules

- Executor accepts only persisted, approved action-job IDs; never accepts arbitrary action JSON from the model or browser.
- It reloads the approval and action payload from the database and verifies the stored hash before execution.
- The executor uses a database role unavailable to the agent worker.
- The ticket MCP server validates schema, tenant scope, action status, approval record, and idempotency key.
- Each action is single-use and transitions atomically from `queued` to `running` to a terminal state.
- Fixture outbox/ticket operations are transactional and idempotent.
- A failed/ambiguous execution is visible to the operator; it is not silently retried.

### 6.3 Threat model and guardrails

| Threat | Required control |
|---|---|
| Prompt injection in inquiry text | Treat the inquiry as untrusted data; fixed instructions and schema; no write tools available to the agent worker; injection regression cases. |
| Prompt injection in retrieved KB content | Treat KB chunks as untrusted data; content cannot change system policy or tool permissions; only allowlisted tool names/schemas are callable. |
| Model invents refund/discount/date/stock promise | Evidence-bounded drafting; structured factual references; deterministic checks for policy-sensitive values; mandatory human review. |
| Replayed approval | Exact revision/action hashes, single-use action state, idempotency key, atomic transition. |
| Draft edited after approval | Immutable revisions; edit invalidates old verification and approval. |
| Cross-tenant access | Tenant-scoped repository methods and database-level isolation tests; never accept tenant ID from untrusted body without deriving/validating it from the session. |
| PII in logs/traces | Redact before persistence and model calls; no inquiry text in span attributes; safe structured logs. |
| Worker crash during execution | Transactional fixture writes, action idempotency, leases, `unknown` state for ambiguous completion. |
| Public demo abuse | Fixture-only seeded actions, no arbitrary public submission, mock LLM, rate limiting, reset procedure. |

### 6.4 PII and data handling

- v1 handles synthetic fixture data only.
- Normal ingestion stores a redacted body; it does not retain an unredacted copy in the ordinary inquiry table.
- Normalize full-width/half-width characters with NFKC before detection.
- Include tested patterns for common Japanese phone numbers, postal codes, and obvious My Number-like sequences. These patterns are a limited detector, not proof of complete Japanese PII removal.
- Do not log raw email addresses, addresses, phone numbers, order identifiers, or message bodies. Use opaque IDs or masked values.
- If local developer fixtures contain synthetic email/order identifiers, label them clearly and keep them out of public logs.
- Real customer data, data-retention commitments, privacy-law compliance, and production PII detection are explicitly outside v1.

---

## 7. MCP server design

Implement three small MCP server packages with narrow tool contracts. In v1 they are launched over stdio by the authorized host process.

| MCP server | Tool | Access | Contract |
|---|---|---|---|
| `kb-mcp` | `search_kb` | Read-only | `{ query, intent?, limit } → [{ article_id, chunk_id, title, excerpt, lexical_rank?, vector_rank?, fused_score }]` |
| `kb-mcp` | `get_article` | Read-only | `{ article_id } → full article and version metadata` |
| `crm-mcp` | `lookup_order` | Read-only | `{ order_no } → synthetic customer/order summary` |
| `ticket-mcp` | `create_ticket` | Write-only; executor host only | `{ approved_action_id } → { ticket_id }` after loading and validating the approved payload server-side. |
| `ticket-mcp` | `add_ticket_note` | Write-only; executor host only | `{ approved_action_id } → { ticket_id, note_id }` after the same approval checks. |

`send_demo_reply` is not an MCP tool. It is an executor-only internal outbox adapter that writes to the fixture `outbox_messages` table. This keeps the tool surface narrow and avoids presenting a simulated delivery as a real channel integration.

Do not pass the entire action payload from the model to `ticket-mcp`. The `approved_action_id` is an internal reference to a persisted action; the MCP server retrieves the canonical payload from the database and validates it.

Rules:

- Input and output schemas are validated at both client and server boundaries.
- Tool errors return structured error results; the caller maps them to typed failure states.
- Tool calls record tool name, risk level, redacted arguments, status, latency, run ID, and trace/span IDs.
- The agent worker starts only read-only MCP servers.
- The executor starts only the write-capable MCP server and does not host an LLM client.
- Tool metadata such as an MCP annotation is descriptive; application authorization is enforced in code and at the persistence boundary, not assumed from metadata.

---

## 8. Japanese knowledge base, retrieval, and response quality

### 8.1 Knowledge base content

Create 20 short Japanese articles for the fictional retailer:

- shipping areas, delivery windows, fees, and delay handling
- return/exchange window and procedure
- invoice issuance and fictional qualified-invoice handling
- warranty and product care
- payment methods and cancellation policy
- manuals/FAQs for three fictional home-goods products

Each article includes:

`article_id, tenant_id, title, intent_tags, body, version, effective_from, effective_to, updated_at, status`

Only active articles are eligible for retrieval. If policies are superseded, retain the old version for audit but exclude it from current-answer retrieval.

### 8.2 Japanese text normalization and lexical retrieval

- Normalize query and document text using NFKC.
- Tokenize Japanese text with kuromoji (or another explicitly selected tokenizer) before indexing.
- Store the tokenized representation in a PostgreSQL full-text-search column with a GIN index.
- Use PostgreSQL full-text ranking (`ts_rank`/`ts_rank_cd`) and refer to it as **lexical retrieval**, not BM25.
- Do not describe `tsvector` as BM25. If true BM25 is later required, implement and benchmark a real BM25 scorer or use a search engine that provides it.

### 8.3 Dense retrieval and hybrid ranking

Dense retrieval is enabled only after the embedding contract is verified:

- Confirm whether ModelRail exposes a compatible embeddings endpoint and usage reporting.
- If yes, route embeddings through ModelRail and record the exact embedding model/version and dimension.
- If no, either add a supported embeddings endpoint to ModelRail or explicitly configure a separate embedding provider. Do not silently bypass the stated inference boundary.
- Keep embedding dimensions/model metadata in the index version. Changing models requires a re-embedding migration.
- For the small fixture corpus, exact vector search is acceptable initially. Add HNSW or another ANN index only when a benchmark demonstrates a need.

Fuse lexical and dense ranks with a documented method such as reciprocal rank fusion (RRF). A fused rank is not a probability. Thresholds are chosen from retrieval evaluation data, not by guessing a score cutoff.

### 8.4 Retrieval evaluation

Label relevant article IDs for each gold inquiry. Report:

- Recall@k (especially Recall@5)
- MRR or nDCG@k
- percentage of cases with no relevant evidence
- retrieval latency
- result quality by intent and edge-case type

When evidence is insufficient, escalate. Never fill gaps with general model knowledge.

### 8.5 Response schema and verification

The model returns a schema similar to:

```ts
type DraftOutput = {
  body: string;
  citedArticleIds: string[];
  factualReferences: Array<{
    factKey: string;
    normalizedValue: string;
    sourceArticleId: string;
    sourceChunkId: string;
  }>;
};
```

The model output contains no action proposal and no tool-call arguments. After the draft passes verification, the deterministic policy engine creates an operator-visible list of allowed actions (for example, `create_ticket` or `send_demo_reply`). The operator selects each action explicitly. The model cannot choose, parameterize, or execute actions.

The verifier performs these checks:

1. Output parses against the schema and size constraints.
2. Every cited article/chunk exists, belongs to the correct tenant, and was included in the run’s retrieved evidence.
3. Every structured factual reference points to a source field/chunk and its normalized value matches the source representation.
4. Extracted numeric values, dates, order states, prices, return periods, and refund/cancellation statements are checked against retrieved evidence and fixture records. Unsupported policy-sensitive values cause a hard failure.
5. Required company signature and mechanical style constraints pass.
6. No banned casual endings or prohibited promises appear.

**Limit of the verifier:** these checks do not prove that every free-form Japanese sentence is semantically entailed by its citation. A separate judge may flag suspected unsupported claims, but an LLM judge is not a security control. This limitation is documented and is one reason human approval remains required.

### 8.6 Japanese writing quality

Mechanical checks can enforce format, required phrases, length, and a short list of obvious register violations. They cannot guarantee natural 丁寧語, correct nuance, or culturally appropriate handling of complaints.

Before publishing the demo:

- Have fixture inquiries, KB articles, expected answers, and a sample of generated drafts reviewed by a competent Japanese speaker.
- Record review status in the dataset metadata.
- Evaluate politeness, clarity, factuality, completeness, and escalation appropriateness separately.
- Avoid claiming that the entire Japanese corpus is native-reviewed unless that review actually occurred.

---

## 9. Data model

Every business-owned row carries `tenant_id`. The demo seeds at least two synthetic tenants so isolation can be tested rather than merely asserted.

Core tables:

- `tenants` — id, name, status
- `users` — id, tenant_id, email, password_hash, role (`operator|admin`), status
- `inquiries` — id, tenant_id, source, subject, redacted_body, normalized_body_hash, order_no_hint, status, created_at
- `customers` — id, tenant_id, synthetic_name, synthetic_contact_ref
- `orders` — id, tenant_id, customer_id, order_no, item, ordered_at, delivery_status
- `kb_articles` — id, tenant_id, title, intent_tags, version, effective dates, status
- `kb_chunks` — id, tenant_id, article_id, chunk_index, text, normalized_text, tokenized_text, embedding, embedding_model, index_version
- `runs` — id, tenant_id, inquiry_id, state, model_route, prompt_version, config_snapshot, usage, cost nullable, trace_id, lease fields, started_at, finished_at, failure_code
- `run_steps` — id, tenant_id, run_id, step_key, attempt, state, input_redacted, output_redacted, prompt_version, model, timing, trace/span IDs, error_code
- `tool_calls` — id, tenant_id, run_id, server, tool, risk_level, args_redacted, status, latency_ms, trace_id, span_id
- `drafts` — id, tenant_id, run_id, revision, body, cited_article_ids, factual_references, verification_json, content_hash, state
- `proposed_actions` — id, tenant_id, draft_id, action_type, canonical_payload, payload_hash, state
- `approvals` — id, tenant_id, draft_id, draft_hash, operator_id, selected_action_ids, policy_version, approved_at, rejected_reason nullable
- `execution_jobs` — id, tenant_id, proposed_action_id, idempotency_key, state, lease fields, attempts, result_json, failure_code
- `tickets` — id, tenant_id, inquiry_id, status, priority, created_at
- `outbox_messages` — id, tenant_id, inquiry_id, draft_revision, body, delivery_state, created_at
- `audit_events` — id, tenant_id, actor_type, actor_id, action, entity_type, entity_id, before_hash, after_hash, trace_id, created_at
- `eval_cases`, `eval_runs`, `eval_results` — versioned evaluation data and reports

### Database invariants

- Unique `(run_id, step_key, attempt)` or an equivalent stable checkpoint key.
- Unique action idempotency key.
- Unique `(draft_id, revision)`.
- Approval foreign key binds to a single draft revision/hash.
- Agent-worker database role has no insert/update/delete permission on tickets or outbox tables.
- Executor database role has only the minimum permissions needed to claim jobs and perform approved fixture actions.
- All repository access requires an authenticated tenant context. Add row-level security if it can be implemented and tested cleanly; regardless, include cross-tenant integration tests at the repository/database boundary.

Do not store raw message content in audit events, tool arguments, or traces.

---

## 10. ModelRail and Kairo integrations

### 10.1 ModelRail — inference gateway

- All chat/completion requests go through ModelRail’s OpenAI-compatible endpoint.
- Use a project-scoped credential provided only to the agent worker. Never expose it to the browser.
- Persist model route, prompt version, request ID, token usage, latency, and provider cost when the gateway supplies them.
- Enforce a local per-run budget and use ModelRail-side budget controls when available. Local limits must stop additional calls even if gateway usage reporting is delayed.
- Configure request timeout, connection timeout, retry policy, and maximum response size.
- ModelRail unavailable → typed `GATEWAY_UNAVAILABLE` failure; operator can retry explicitly.
- `MOCK_LLM=1` uses deterministic, case-keyed fixture responses and is clearly labelled as mock. It is not represented as a live model result.
- Add an integration contract test for chat completion, usage fields, error response, timeout, and optional embeddings before relying on any specific ModelRail capability.

### 10.2 Kairo — tracing and observability

- Use OpenTelemetry JS with one root span per run: `benri.run`.
- Child spans: `classify`, `retrieve`, `lookup_order`, `policy_decision`, `draft`, `verify`, and each MCP call.
- Propagate trace context across API, worker, and executor boundaries where supported.
- Store the trace ID on the run and render a `View trace` link only when a configured Kairo base URL is available.
- Never attach raw inquiry text, email, phone, address, order number, or full prompts as span attributes.
- Export asynchronously. Kairo being down must not block or fail a run.
- Capture the local trace even when the Kairo exporter is disabled, so tests and local debugging remain useful.

### 10.3 Integration modes

| Mode | ModelRail | Kairo | Purpose |
|---|---|---|---|
| Local mock | Disabled | Disabled/optional | Fast development and CI; deterministic fixtures. |
| Local live | Required | Optional | Real model behavior and cost/latency measurements. |
| Deployed demo | Disabled | Optional | Safe public walkthrough using seeded cases only. |
| Staging validation | Required | Required | Verify end-to-end gateway, telemetry, and failure handling. |

---

## 11. Evaluation and regression harness

The evaluation harness is a first-class deliverable. Keep it independent from the web UI so it can run locally and in CI.

### 11.1 Gold dataset

Create 50 versioned JSONL cases authored and maintained by the project owner:

| Group | Count | Examples |
|---|---:|---|
| Routine | 24 | Delivery status, product use, invoice, returns within policy, stock lookup. |
| Edge cases | 10 | Mixed Japanese/English, typos, missing order number, ambiguous phrasing, full-width characters. |
| Must-escalate | 8 | Complaints, no KB evidence, identity ambiguity, legal/safety-sensitive request. |
| Adversarial | 8 | Prompt injection, request to invent a discount/date, PII fishing, tool-output injection, cross-tenant probing. |
| **Total** | **50** | |

Each case includes:

- case ID and dataset version
- synthetic inquiry text
- expected intent and urgency
- expected escalation boolean and reason code
- acceptable article IDs / relevance labels
- required or forbidden factual claims
- expected allowed action types
- adversarial flags
- human review notes for Japanese-language quality

Do not put hidden personal/customer data in the dataset. Keep the cases synthetic.

### 11.2 Metrics

**Classification**
- accuracy and macro-F1
- confusion matrix by intent

**Escalation**
- precision, recall, and F1
- false-safe count (a case that should escalate but receives a draft)

**Retrieval**
- Recall@5, MRR or nDCG@5, and no-evidence detection

**Grounding and policy**
- citation precision/recall
- factual-reference validity
- unsupported policy-sensitive fact count
- verification pass/fail rate

**Agent/tool safety**
- number of write calls initiated by the agent worker (must be zero)
- number of unapproved executor actions (must be zero)
- duplicate fixture writes under retry/crash tests (must be zero)
- cross-tenant reads/writes (must be zero)

**Operational**
- median and p95 run latency
- token usage and cost per run when gateway usage supports measurement
- step retry count and failure reason distribution

**Japanese response quality**
- human/judge rubric scores for politeness, clarity, correctness, completeness, and escalation appropriateness
- judge model, rubric version, and calibration set recorded with each report

### 11.3 CI gates

CI uses deterministic mocks and should be reproducible. Recommended initial gates:

- no unapproved write attempt, no agent-worker write capability, no cross-tenant access, and no duplicate action — hard zero-tolerance gates
- escalation recall target at least 0.90; with eight must-escalate examples, the report must show the actual count (for example, 8/8), not only a rounded percentage
- intent accuracy/macro-F1 target initially 0.85, reviewed against the observed confusion matrix
- retrieval Recall@5 target initially 0.90 on labelled retrieval cases
- all deterministic schema, citation-reference, and policy-sensitive fact checks pass for the gold set

Thresholds are starting targets, not validated guarantees. Adjust them only with a recorded rationale and dataset version change.

### 11.4 LLM-as-judge policy

- Use an independent judge route/model where practical.
- Version the judge prompt and rubric.
- Calibrate the judge against a human-scored subset; report agreement and disagreements.
- Use judge scores as a quality signal, not as the only CI security gate.
- Do not make pull-request CI depend on an unpinned, nondeterministic judge response. Run live-model evaluation as a separate command or controlled release job and preserve the model route/version and raw score details.

### 11.5 Negative controls

Run at least these controlled degradations:

- retrieval disabled
- wrong or stale KB version
- deliberately truncated evidence
- malformed tool output
- ModelRail unavailable
- Kairo unavailable
- worker crash after a checkpoint
- executor crash around an idempotent write

The report should show which metrics degrade and why. A no-retrieval run should lose retrieval coverage and either escalate safely or fail grounding checks. Do not manufacture a failing CI run just to claim a failure was committed; make the negative-control assertion itself reproducible.

---

## 12. API and UI

### 12.1 API endpoints

Authentication:

- `POST /auth/login`
- `POST /auth/logout`
- `GET /auth/session`

Inquiries and runs:

- `GET /inquiries?state=`
- `GET /inquiries/:id`
- `POST /inquiries` — local/dev only; redacts before persistence
- `POST /inquiries/:id/runs` — idempotent run request
- `GET /runs/:id` — run summary, steps, tool calls, draft, verification, trace metadata

Draft review:

- `GET /drafts/:id`
- `POST /drafts/:id/revisions` — creates a new immutable revision and triggers verification
- `POST /drafts/:id/approve` — requires revision ID and explicitly selected action IDs
- `POST /drafts/:id/reject` — requires a reason

Operations:

- `GET /tickets`
- `GET /outbox`
- `GET /kb/articles`
- `POST /kb/articles` / `PATCH /kb/articles/:id` — admin only
- `POST /kb/reindex` — admin only, versioned index build
- `GET /evals/runs`
- `GET /evals/runs/:id`
- `GET /audit?entity_type=&entity_id=`
- `GET /health`

All endpoints are tenant-scoped and return stable error codes. Mutating endpoints accept idempotency keys where retry is possible.

### 12.2 UI screens

Keep the operator UI dense and functional rather than promotional.

1. **Queue:** state, intent, urgency, created time, and escalation reason.
2. **Run detail:** step/checkpoint timeline, evidence, tool calls, latency, ModelRail usage, trace link.
3. **Review:** original redacted inquiry, generated draft, article excerpts, verification results, proposed actions, edit/re-verify, per-action approve/reject.
4. **Escalations:** Japanese handoff summary, reason code, evidence inspected, close/assign action.
5. **Admin:** KB editor/reindex, evaluation reports, audit viewer.

Japanese is the default UI language; English is an operator-facing toggle. Do not translate customer Japanese source text automatically in the initial product workflow.

---

## 13. Repository layout

```text
benri-agent/
  apps/
    api/                 # Fastify API, auth, tenant context, approval transaction
    web/                 # Next.js operator UI
    agent-worker/        # read-only agent runtime and checkpoint recovery
    executor-worker/     # approved action executor; no LLM client
  packages/
    agent-core/          # pipeline, policies, state machine, schemas
    retrieval/           # kuromoji normalization, lexical/dense ranking, RRF
    mcp-kb/              # read-only KB MCP server
    mcp-crm/             # read-only order/CRM MCP server
    mcp-ticket/          # executor-only write MCP server
    db/                  # Drizzle schema, migrations, repository contracts
    observability/       # OTel setup and trace helpers
    modelrail-client/    # gateway client, budgets, usage parsing
    japanese-quality/    # deterministic format/register checks
  evals/
    dataset/
    runner/
    judges/
    reports/
  fixtures/
    inquiries/
    kb/
    crm/
  docs/
    architecture.md
    threat-model.md
    eval-methodology.md
    runbook.md
  CLAUDE.md
  AGENT-LOG.md
  README.md
  docker-compose.yml
  pnpm-workspace.yaml
```

Do not create a package merely to make the repository look like a microservice architecture. Each package must have a narrow contract and a testable reason to exist.

---

## 14. Coding-agent workflow and engineering evidence

This section produces credible evidence of how the project was built. It must record the real workflow, including mistakes.

### 14.1 Claude Code workflow

- Use Claude Code as the primary coding assistant for the repository.
- Keep `CLAUDE.md` specific: architecture boundaries, commands, test policy, package ownership, security invariants, and rules against unmeasured claims.
- Work in small issue-sized tasks. Review the complete diff, run relevant tests, and commit only after verification.
- Do not accept generated code merely because it compiles.
- Keep prompts and task notes where useful, but do not publish secrets or private client data.

### 14.2 `AGENT-LOG.md`

Use one entry per substantive session:

```md
## YYYY-MM-DD — <task>
Agent: Claude Code | Codex | Other
Task: <issue-sized objective>
Produced: <summary of the diff>
Accepted: <what was kept and why>
Changed/rejected: <specific incorrect or unsafe output>
Verification: <tests, commands, manual checks>
Commit: <hash>
```

Minimum evidence target before writing the portfolio case study:

- at least 10 dated entries
- at least 3 substantive corrections or rejected suggestions
- test/verification evidence for each accepted change
- one documented worker-recovery or approval-boundary bug caught during review

### 14.3 Independent review

For security-sensitive changes, use a review checklist or a second independent reviewer. The reviewer must inspect the diff and tests, not just summarize the implementation. Record disagreements and the final decision. LLM review is advisory; the author remains responsible for validating the code.

### 14.4 What can honestly be claimed

Benri demonstrates:

- practical Claude Code use and code review discipline
- an application-agent harness with bounded execution, checkpointing, tool permissions, and recovery
- an evaluation dataset, deterministic regression checks, and calibrated LLM-as-judge analysis
- Japanese retrieval and evidence-grounded response generation
- MCP tool integration, backend service boundaries, audit, and OpenTelemetry

Do not claim that Benri is a general coding-agent harness or that it autonomously develops software. Its runtime is an operational/customer-support agent. The `AGENT-LOG.md` demonstrates the coding-agent workflow used to build it.

---

## 15. Deployment and demo plan

### 15.1 Local development

`docker compose up --build` should run the core stack with PostgreSQL, API, web, agent worker, and executor. Mock mode is the default. ModelRail and Kairo are configured as optional external endpoints.

### 15.2 Public demo safety

If a public deployment is created:

- use a seeded fictional tenant and fixed sample cases only
- disable arbitrary inquiry submission
- disable all live model calls
- use mock responses keyed to sample inquiry IDs
- allow only bounded fixture actions and reset the demo state on a schedule
- apply rate limits and secure demo credentials/session handling
- label every result as simulated
- provide no access to ModelRail credentials, Kairo credentials, or internal admin accounts

A public mock demo is useful for showing the workflow, but it must not be described as a live LLM deployment. A local live mode demonstrates actual ModelRail inference.

### 15.3 Demo walkthrough (approximately two minutes)

1. Open the seeded queue and select a delivery-status inquiry.
2. Run the agent; show classification, retrieved Japanese article excerpts, order lookup, and checkpoints.
3. Open the draft; show citations and verifier output; edit one sentence.
4. Approve only `send_demo_reply` and `create_ticket`; show the exact actions in the approval summary.
5. Show the fixture outbox and ticket, then the audit events and Kairo trace (if configured).
6. Open a complaint and a no-evidence case; show escalation with no customer-facing draft.
7. Open the injection case; show that the agent has no write capability and that no unapproved action exists.
8. Show the evaluation report and one negative-control result.

---

## 16. Delivery plan and scope gates

The original three-week part-time estimate is too optimistic for this amount of backend, security, Japanese-content, evaluation, and deployment work. Use the following capability gates rather than sacrificing correctness to meet an arbitrary date.

### Phase A — Vertical slice

- monorepo, compose, database schema, fixture seed
- one seeded inquiry from queue to mock draft
- minimal review UI
- state machine and persisted run steps

**Exit:** clean startup and one complete mock run.

### Phase B — Trust boundary

- agent-worker/executor-worker split and separate DB permissions
- read-only MCP tools and executor-only ticket MCP
- payload-specific approvals, immutable draft revisions, idempotency
- prompt/tool injection and cross-tenant tests

**Exit:** tests prove the agent cannot write and executor cannot act without a persisted approval.

### Phase C — Japanese retrieval and quality

- Japanese normalization/tokenization and lexical retrieval
- embeddings only after the ModelRail capability spike
- evidence-bounded drafting and deterministic verification
- escalation policies and Japanese fixture review

**Exit:** retrieval and escalation metrics are generated from labelled cases.

### Phase D — Reliability and evaluation

- leases, heartbeats, checkpoint resume, failure handling
- 50-case gold set and evaluation runner
- negative controls, CI gates, LLM-as-judge calibration
- OpenTelemetry and Kairo integration

**Exit:** worker-crash, gateway-down, and action-replay tests pass; report is reproducible.

### Phase E — Portfolio release

- public mock demo or recorded local demo
- bilingual README, architecture/threat-model/eval docs
- measured results and limitations
- Japanese Zenn article after the implementation and measurements are complete

**Exit:** every public statement is supported by repository evidence.

If time is constrained, cut the public deployment, bilingual UI, or optional dense retrieval before cutting approval security, recovery, tests, or evaluation integrity.

---

## 17. Portfolio positioning

### 17.1 Strongest project narrative

“Benri is a Japanese customer-operations agent built around a strict separation between model reasoning and privileged execution. It retrieves policy evidence, drafts business-Japanese responses, checkpoints each stage, and stops for human approval. A separate executor processes only immutable, approved action payloads. The system is evaluated against a versioned Japanese gold set and observed through OpenTelemetry, ModelRail, and Kairo.”

### 17.2 Resume bullet — only after implementation and measurement

**English draft:**

> Built Benri Agent, a TypeScript/Node.js human-in-the-loop operations agent using MCP, PostgreSQL, and pgvector/lexical retrieval. Designed a read-only agent runtime with checkpoint recovery and a separate approval-gated executor; added a versioned Japanese evaluation suite, CI safety gates, and OpenTelemetry tracing through Kairo. Routed chat inference through ModelRail. Reported classification, escalation, retrieval, latency, and cost results from reproducible runs.

Replace any technology or result that was not actually implemented. Add numeric metrics only from committed evaluation reports.

**Japanese one-liner draft:**

> 日本語の顧客問い合わせを対象に、根拠となるナレッジを参照して返信案を作成する承認制AIエージェント「Benri」をTypeScript/Node.jsで開発。読み取り専用のエージェント実行環境と承認済み操作のみを処理する実行環境を分離し、MCP、評価テスト、チェックポイントによる復旧、OpenTelemetryによる可観測性を実装。

Use this only after the described features exist. Adjust the wording if dense retrieval, Kairo, or ModelRail is not actually integrated.

### 17.3 Job alignment

- **Cogent Labs:** strongest evidence is the coding-agent workflow log, application-agent harness, checkpoints, tool permissions, independent review, gold cases, evaluation methodology, and TypeScript/Node implementation. Do not present a customer-support agent as if it were a coding-agent product.
- **Rapyuta Robotics:** strongest evidence is the MCP services, retrieval pipeline, backend boundaries, worker recovery, cloud/deployment discipline, CI, and operational observability. Use the project to explain how the same engineering principles transfer to robotics/logistics workflows.

Treat job descriptions as moving targets. Re-check the exact posting before tailoring an application; this spec should remain a coherent project, not a collection of keywords from one vacancy.

---

## 18. Configuration contract

Keep configuration centralized and validate it at process startup. Example variables:

```dotenv
NODE_ENV=development
DATABASE_URL=postgres://...
SESSION_SECRET=...
DEMO_MODE=1
MOCK_LLM=1
MODELRAIL_BASE_URL=
MODELRAIL_API_KEY=
MODELRAIL_CHAT_MODEL=
MODELRAIL_EMBEDDING_MODEL=
BENRI_MAX_MODEL_CALLS_PER_RUN=
BENRI_MAX_TOOL_CALLS_PER_RUN=
BENRI_MAX_RUN_DURATION_MS=
BENRI_MAX_INPUT_TOKENS=
BENRI_MAX_OUTPUT_TOKENS=
BENRI_MAX_COST_PER_RUN_JPY=
KAIRO_OTLP_ENDPOINT=
KAIRO_TRACE_UI_BASE_URL=
```

The exact ModelRail endpoint path and response fields must be verified against the deployed gateway. Do not assume that chat completions, embeddings, model listing, or cost reporting all exist just because one OpenAI-compatible endpoint works.

Never commit actual secrets. Provide `.env.example` with placeholder values only.

---

## 19. Open decisions to resolve during implementation

These are explicit implementation decisions, not reasons to block the project:

1. **ModelRail embeddings:** run a capability spike first. If unsupported, ship lexical retrieval before adding a separate embeddings dependency.
2. **Japanese corpus review:** obtain competent Japanese-language review for fixture content and representative drafts; disclose what was reviewed.
3. **MCP SDK transport:** smoke-test the selected v2 package and stdio transport in the exact Node runtime used by Docker and local development.
4. **Tenant enforcement:** implement tenant-scoped repositories first; add PostgreSQL RLS if the context propagation and tests remain straightforward.
5. **Hosting:** choose a host only after the local Compose deployment is repeatable. Do not let free-tier constraints weaken security or reliability.
6. **Cost ceiling:** choose a default only after measuring the selected ModelRail route and deciding an acceptable development budget.

---

## 20. Definition of an honest release

Before publishing v1, verify the following:

- [ ] All stated components exist in the repository and are exercised by tests.
- [ ] The agent worker has no write permissions and cannot invoke the executor.
- [ ] Approval is bound to the exact draft revision and selected action payloads.
- [ ] Retries, leases, checkpoint recovery, and action idempotency are tested.
- [ ] Japanese retrieval metrics are computed from labelled cases.
- [ ] The verifier’s limits are documented; no claim of perfect semantic grounding is made.
- [ ] Japanese content review status is stated accurately.
- [ ] ModelRail and Kairo failure modes are tested, not merely diagrammed.
- [ ] Mock mode is labelled as mock everywhere it appears.
- [ ] Public demo performs no real external action and accepts no arbitrary public customer data.
- [ ] Evaluation reports contain dataset, prompt, model, and index versions.
- [ ] README, Zenn, and CV contain measured values only.
- [ ] `AGENT-LOG.md` records actual agent work, accepted changes, rejected output, and verification commands.

**Final engineering principle:** the model may interpret, retrieve, summarize, and draft. The application decides what is allowed. A human approves the exact change. A separate executor performs only that approved change. Every transition is recoverable, observable, and testable.
