-- sqlc query set for the PostgreSQL path (SPEC §13). These mirror the
-- locally verified SQLite statements. STATUS: provided, UNVERIFIED (no
-- PostgreSQL on this machine); generation with sqlc is verified.

-- name: GetInquiry :one
SELECT id, tenant_id, source, subject, redacted_body, normalized_body_hash, order_no_hint, status, created_at
FROM inquiries WHERE tenant_id = $1 AND id = $2;

-- name: ListInquiries :many
SELECT id, tenant_id, source, subject, redacted_body, normalized_body_hash, order_no_hint, status, created_at
FROM inquiries WHERE tenant_id = $1 AND ($2 = '' OR status = $2) ORDER BY created_at;

-- name: InsertInquiry :exec
INSERT INTO inquiries (id, tenant_id, source, subject, redacted_body, normalized_body_hash, order_no_hint, status, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (id) DO NOTHING;

-- name: FindOrderByNo :one
SELECT id, tenant_id, customer_id, order_no, item, ordered_at, delivery_status
FROM orders WHERE tenant_id = $1 AND order_no = $2;

-- name: ListActiveArticles :many
SELECT id, tenant_id, title, intent_tags, body, version, status, updated_at
FROM kb_articles WHERE tenant_id = $1 AND status = 'active' ORDER BY id;

-- name: CreateRun :exec
INSERT INTO runs (id, tenant_id, inquiry_id, state, prompt_version, model_route, trace_id, config_snapshot, idempotency_key, created_at)
VALUES ($1, $2, $3, 'queued', $4, $5, $6, $7, $8, $9)
ON CONFLICT (idempotency_key) DO NOTHING;

-- name: ClaimNextRun :one
UPDATE runs SET state = 'running', lease_owner = $1, lease_expires_at = $2, heartbeat_at = $3, attempt_count = attempt_count + 1
WHERE id = (SELECT id FROM runs WHERE state = 'queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING id, tenant_id, inquiry_id, state;

-- name: InsertDraft :exec
INSERT INTO drafts (id, tenant_id, run_id, revision, body, cited_article_ids, factual_references, verification_json, content_hash, state, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);

-- name: InsertApproval :exec
INSERT INTO approvals (id, tenant_id, draft_id, draft_revision, draft_hash, operator_id, selected_action_ids, policy_version, approved_at, rejected_reason)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: InsertExecutionJob :one
INSERT INTO execution_jobs (id, tenant_id, proposed_action_id, idempotency_key, state, created_at)
VALUES ($1, $2, $3, $4, 'queued', $5)
ON CONFLICT (idempotency_key) DO UPDATE SET idempotency_key = EXCLUDED.idempotency_key
RETURNING id, tenant_id, proposed_action_id, idempotency_key, state;

-- name: ClaimNextJob :one
UPDATE execution_jobs SET state = 'running', lease_owner = $1, lease_expires_at = $2, heartbeat_at = $3, attempts = attempts + 1
WHERE id = (SELECT id FROM execution_jobs WHERE state = 'queued' ORDER BY created_at FOR UPDATE SKIP LOCKED LIMIT 1)
RETURNING id, tenant_id, proposed_action_id, idempotency_key, state;

-- name: InsertTicket :exec
INSERT INTO tickets (id, tenant_id, inquiry_id, status, priority, source_action_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: InsertOutbox :exec
INSERT INTO outbox_messages (id, tenant_id, inquiry_id, draft_revision, body, delivery_state, source_action_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- name: InsertAudit :exec
INSERT INTO audit_events (id, tenant_id, actor_type, actor_id, action, entity_type, entity_id, before_hash, after_hash, trace_id, created_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11);
