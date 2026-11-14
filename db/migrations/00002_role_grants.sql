-- Per-role least-privilege grants (SPEC §9). Roles are LOGIN roles created
-- by the deployment (passwords via secret management, never in the repo).
-- The capability model:
--   benri_agent:     runs/steps/tool_calls/drafts/proposed_actions writes;
--                    NO approvals, NO execution_jobs insert, NO tickets/outbox.
--   benri_api:       approvals + execution_jobs insert; NO tickets/outbox.
--   benri_executor:  execution_jobs claim/update + outbox insert;
--                    NO direct tickets insert.
--   benri_kb_mcp:    read-only KB. benri_crm_mcp: read-only CRM.
--   benri_ticket_mcp: tickets/ticket_notes insert (after approval checks in code).
-- +goose Up
-- +goose StatementBegin
DO $$
BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_agent') THEN CREATE ROLE benri_agent LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_api') THEN CREATE ROLE benri_api LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_executor') THEN CREATE ROLE benri_executor LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_kb_mcp') THEN CREATE ROLE benri_kb_mcp LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_crm_mcp') THEN CREATE ROLE benri_crm_mcp LOGIN; END IF;
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'benri_ticket_mcp') THEN CREATE ROLE benri_ticket_mcp LOGIN; END IF;
END $$;
-- +goose StatementEnd
-- These are fixture-only Compose credentials. Production deployments must
-- provision the roles and passwords through their secret-management layer.
ALTER ROLE benri_agent PASSWORD 'benri-dev-only';
ALTER ROLE benri_api PASSWORD 'benri-dev-only';
ALTER ROLE benri_executor PASSWORD 'benri-dev-only';
ALTER ROLE benri_kb_mcp PASSWORD 'benri-dev-only';
ALTER ROLE benri_crm_mcp PASSWORD 'benri-dev-only';
ALTER ROLE benri_ticket_mcp PASSWORD 'benri-dev-only';
-- agent
GRANT SELECT, INSERT, UPDATE ON runs, run_steps, tool_calls, drafts, proposed_actions, audit_events TO benri_agent;
GRANT SELECT ON tenants, users, inquiries, customers, orders, kb_articles, kb_chunks TO benri_agent;
GRANT UPDATE (status) ON inquiries TO benri_agent;
REVOKE INSERT, UPDATE, DELETE ON approvals, execution_jobs, tickets, ticket_notes, outbox_messages FROM benri_agent;
-- api
GRANT SELECT, INSERT, UPDATE ON approvals, execution_jobs, drafts, proposed_actions, inquiries, kb_articles, kb_chunks, sessions, audit_events TO benri_api;
GRANT SELECT ON tenants, users, customers, orders, runs, run_steps, tool_calls, tickets, ticket_notes, outbox_messages, eval_runs TO benri_api;
GRANT INSERT ON runs TO benri_api;
REVOKE INSERT, UPDATE, DELETE ON tickets, ticket_notes, outbox_messages FROM benri_api;
-- executor
GRANT SELECT, UPDATE ON execution_jobs TO benri_executor;
GRANT INSERT, SELECT ON outbox_messages TO benri_executor;
GRANT SELECT ON proposed_actions, drafts, approvals, runs TO benri_executor;
GRANT UPDATE (state) ON proposed_actions TO benri_executor;
REVOKE INSERT, UPDATE, DELETE ON tickets, ticket_notes FROM benri_executor;
-- kb / crm (read-only)
GRANT SELECT ON kb_articles, kb_chunks TO benri_kb_mcp;
GRANT SELECT ON customers, orders TO benri_crm_mcp;
-- ticket mcp (the only ticket writer)
GRANT INSERT, SELECT ON tickets, ticket_notes TO benri_ticket_mcp;
GRANT SELECT ON proposed_actions, drafts, approvals TO benri_ticket_mcp;
GRANT UPDATE (state) ON proposed_actions TO benri_ticket_mcp;
GRANT INSERT, SELECT ON audit_events TO benri_ticket_mcp;
-- +goose Down
REVOKE ALL ON ALL TABLES IN SCHEMA public FROM benri_agent, benri_api, benri_executor, benri_kb_mcp, benri_crm_mcp, benri_ticket_mcp;
