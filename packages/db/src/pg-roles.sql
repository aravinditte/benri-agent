-- Database role permissions for the PostgreSQL path (SPEC §3.3, §9).
-- STATUS: UNVERIFIED — no PostgreSQL on the build machine. Apply in Compose.
-- Agent role: read KB/CRM fixtures, write only its own run bookkeeping.
CREATE ROLE benri_agent LOGIN PASSWORD 'change-me';
CREATE ROLE benri_executor LOGIN PASSWORD 'change-me';
CREATE ROLE benri_api LOGIN PASSWORD 'change-me';
-- agent: NO write on tickets / outbox_messages / approvals / execution_jobs
REVOKE ALL ON tickets, ticket_notes, outbox_messages, approvals, execution_jobs FROM benri_agent;
GRANT SELECT ON tenants, kb_articles, kb_chunks, customers, orders, inquiries TO benri_agent;
GRANT SELECT, INSERT, UPDATE ON runs, run_steps, tool_calls, drafts, proposed_actions, audit_events TO benri_agent;
-- executor: claim jobs, write fixture outputs only
GRANT SELECT, UPDATE ON execution_jobs TO benri_executor;
GRANT SELECT ON proposed_actions, approvals, drafts TO benri_executor;
GRANT INSERT, SELECT ON tickets, ticket_notes, outbox_messages TO benri_executor;
GRANT INSERT ON audit_events TO benri_executor;
-- api: approval transaction owner
GRANT SELECT, INSERT, UPDATE ON inquiries, runs, drafts, proposed_actions, approvals, execution_jobs, audit_events TO benri_api;
GRANT SELECT ON kb_articles, kb_chunks, tickets, outbox_messages, eval_runs TO benri_api;
GRANT SELECT, INSERT, UPDATE ON kb_articles, kb_chunks TO benri_api;
