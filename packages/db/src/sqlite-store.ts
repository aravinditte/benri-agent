// Portable store backed by node:sqlite (DatabaseSync). This is the locally
// verified persistence path on machines without PostgreSQL/Docker.
// It implements the same repository contracts as the Drizzle/PostgreSQL path
// (see schema.ts / pg-schema.sql, which are provided but UNVERIFIED here).
//
// Capability model (SPEC §3.3/§6): a store handle is created with a role.
// - "agent"    : read-mostly; writes allowed only to runs/run_steps/tool_calls/
//                drafts/proposed_actions/audit for its own runs. Structurally
//                REJECTS writes to tickets, outbox_messages, approvals and
//                execution_jobs (CapabilityError).
// - "executor" : may claim execution jobs and write tickets/outbox; cannot
//                create runs, drafts or approvals.
// - "api"      : may write approvals + execution_jobs (the approval
//                transaction), inquiries, KB; cannot write tickets/outbox
//                directly (only the executor does that).
// - "full"     : test/seed only.
import { DatabaseSync } from "node:sqlite";
import { randomUUID } from "node:crypto";
import { CapabilityError, NotFoundError } from "./errors.js";

export type StoreRole = "agent" | "executor" | "api" | "full";
export const nowIso = () => new Date().toISOString();
export const newId = (prefix: string) => `${prefix}_${randomUUID().slice(0, 12)}`;

const DDL = `
CREATE TABLE IF NOT EXISTS tenants (id TEXT PRIMARY KEY, name TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active');
CREATE TABLE IF NOT EXISTS users (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, email TEXT NOT NULL UNIQUE, password_hash TEXT NOT NULL, role TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'active');
CREATE TABLE IF NOT EXISTS sessions (token_hash TEXT PRIMARY KEY, user_id TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS inquiries (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, source TEXT NOT NULL DEFAULT 'seed', subject TEXT NOT NULL, redacted_body TEXT NOT NULL, normalized_body_hash TEXT NOT NULL, order_no_hint TEXT, status TEXT NOT NULL DEFAULT 'new', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS customers (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, synthetic_name TEXT NOT NULL, synthetic_contact_ref TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS orders (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, customer_id TEXT NOT NULL, order_no TEXT NOT NULL, item TEXT NOT NULL, ordered_at TEXT NOT NULL, delivery_status TEXT NOT NULL, UNIQUE(tenant_id, order_no));
CREATE TABLE IF NOT EXISTS kb_articles (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, title TEXT NOT NULL, intent_tags TEXT NOT NULL, body TEXT NOT NULL, version INTEGER NOT NULL DEFAULT 1, effective_from TEXT, effective_to TEXT, status TEXT NOT NULL DEFAULT 'active', updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS kb_chunks (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, article_id TEXT NOT NULL, chunk_index INTEGER NOT NULL, text TEXT NOT NULL, tokenized_text TEXT NOT NULL, embedding TEXT, embedding_model TEXT, index_version TEXT NOT NULL DEFAULT 'lexical-v1');
CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'queued', model_route TEXT, prompt_version TEXT, config_snapshot TEXT, usage_json TEXT, cost_jpy REAL, trace_id TEXT, lease_owner TEXT, lease_expires_at TEXT, heartbeat_at TEXT, attempt_count INTEGER NOT NULL DEFAULT 0, last_error_code TEXT, idempotency_key TEXT UNIQUE, escalation_reason TEXT, handoff_summary TEXT, started_at TEXT, finished_at TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS run_steps (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT NOT NULL, step_key TEXT NOT NULL, attempt INTEGER NOT NULL, state TEXT NOT NULL, input_redacted TEXT, output_redacted TEXT, prompt_version TEXT, model TEXT, started_at TEXT, finished_at TEXT, duration_ms INTEGER, trace_id TEXT, span_id TEXT, error_code TEXT, UNIQUE(run_id, step_key, attempt));
CREATE TABLE IF NOT EXISTS tool_calls (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT, server TEXT NOT NULL, tool TEXT NOT NULL, risk_level TEXT NOT NULL, args_redacted TEXT, status TEXT NOT NULL, latency_ms INTEGER, trace_id TEXT, span_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS drafts (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, run_id TEXT NOT NULL, revision INTEGER NOT NULL, body TEXT NOT NULL, cited_article_ids TEXT NOT NULL, factual_references TEXT NOT NULL, verification_json TEXT, content_hash TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending_review', created_at TEXT NOT NULL, UNIQUE(run_id, revision));
CREATE TABLE IF NOT EXISTS proposed_actions (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, draft_id TEXT NOT NULL, run_id TEXT, action_type TEXT NOT NULL, canonical_payload TEXT NOT NULL, payload_hash TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'proposed', created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS approvals (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, draft_id TEXT NOT NULL, draft_revision INTEGER NOT NULL, draft_hash TEXT NOT NULL, operator_id TEXT NOT NULL, selected_action_ids TEXT NOT NULL, policy_version TEXT NOT NULL, approved_at TEXT NOT NULL, rejected_reason TEXT);
CREATE TABLE IF NOT EXISTS execution_jobs (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, proposed_action_id TEXT NOT NULL UNIQUE, idempotency_key TEXT NOT NULL UNIQUE, state TEXT NOT NULL DEFAULT 'queued', lease_owner TEXT, lease_expires_at TEXT, heartbeat_at TEXT, attempts INTEGER NOT NULL DEFAULT 0, result_json TEXT, failure_code TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS tickets (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, status TEXT NOT NULL DEFAULT 'open', priority TEXT NOT NULL DEFAULT 'normal', source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS ticket_notes (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, ticket_id TEXT NOT NULL, body TEXT NOT NULL, source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS outbox_messages (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, inquiry_id TEXT NOT NULL, draft_revision INTEGER NOT NULL, body TEXT NOT NULL, delivery_state TEXT NOT NULL DEFAULT 'sent_demo', source_action_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audit_events (id TEXT PRIMARY KEY, tenant_id TEXT NOT NULL, actor_type TEXT NOT NULL, actor_id TEXT, action TEXT NOT NULL, entity_type TEXT NOT NULL, entity_id TEXT NOT NULL, before_hash TEXT, after_hash TEXT, trace_id TEXT, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS eval_runs (id TEXT PRIMARY KEY, dataset_version TEXT, prompt_version TEXT, model TEXT, index_version TEXT, report_json TEXT, created_at TEXT NOT NULL);
`;

type Row = Record<string, unknown>;

export class BenriStore {
  readonly role: StoreRole;
  private db: DatabaseSync;

  constructor(dbPath: string, role: StoreRole = "full") {
    this.role = role;
    this.db = new DatabaseSync(dbPath);
    this.db.exec("PRAGMA journal_mode = WAL;");
    this.db.exec(DDL);
  }

  close() { this.db.close(); }

  private guard(table: string) {
    const writeTables: Record<string, StoreRole[]> = {
      tickets: ["executor", "full"],
      ticket_notes: ["executor", "full"],
      outbox_messages: ["executor", "full"],
      approvals: ["api", "full"],
      execution_jobs: ["api", "executor", "full"],
    };
    const allowed = writeTables[table];
    if (allowed && !allowed.includes(this.role)) {
      throw new CapabilityError(`store role "${this.role}" cannot write to ${table}`);
    }
  }

  private run(sql: string, ...params: unknown[]) {
    return this.db.prepare(sql).run(...(params as never[]));
  }
  private get<T = Row>(sql: string, ...params: unknown[]): T | undefined {
    return this.db.prepare(sql).get(...(params as never[])) as T | undefined;
  }
  private all<T = Row>(sql: string, ...params: unknown[]): T[] {
    return this.db.prepare(sql).all(...(params as never[])) as T[];
  }
  transaction<T>(fn: () => T): T {
    this.db.exec("BEGIN IMMEDIATE");
    try { const r = fn(); this.db.exec("COMMIT"); return r; }
    catch (e) { this.db.exec("ROLLBACK"); throw e; }
  }

  // ---- tenants / users / sessions ----
  insertTenant(id: string, name: string) { this.run("INSERT OR IGNORE INTO tenants(id,name) VALUES(?,?)", id, name); }
  insertUser(u: { id: string; tenant_id: string; email: string; password_hash: string; role: string }) {
    this.run("INSERT OR IGNORE INTO users(id,tenant_id,email,password_hash,role) VALUES(?,?,?,?,?)", u.id, u.tenant_id, u.email, u.password_hash, u.role);
  }
  findUserByEmail(email: string) { return this.get("SELECT * FROM users WHERE email=?", email); }
  findUserById(id: string) { return this.get("SELECT * FROM users WHERE id=?", id); }
  createSession(tokenHash: string, userId: string) { this.run("INSERT INTO sessions(token_hash,user_id,created_at) VALUES(?,?,?)", tokenHash, userId, nowIso()); }
  findUserBySession(tokenHash: string) {
    return this.get("SELECT u.* FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=?", tokenHash);
  }
  deleteSession(tokenHash: string) { this.run("DELETE FROM sessions WHERE token_hash=?", tokenHash); }

  // ---- inquiries ----
  insertInquiry(i: { id: string; tenant_id: string; source?: string; subject: string; redacted_body: string; normalized_body_hash: string; order_no_hint?: string | null; status?: string }) {
    this.run("INSERT OR IGNORE INTO inquiries(id,tenant_id,source,subject,redacted_body,normalized_body_hash,order_no_hint,status,created_at) VALUES(?,?,?,?,?,?,?,?,?)",
      i.id, i.tenant_id, i.source ?? "seed", i.subject, i.redacted_body, i.normalized_body_hash, i.order_no_hint ?? null, i.status ?? "new", nowIso());
    return this.getInquiry(i.tenant_id, i.id)!;
  }
  getInquiry(tenantId: string, id: string) { return this.get<Row>("SELECT * FROM inquiries WHERE tenant_id=? AND id=?", tenantId, id); }
  listInquiries(tenantId: string, status?: string) {
    return status
      ? this.all("SELECT * FROM inquiries WHERE tenant_id=? AND status=? ORDER BY created_at", tenantId, status)
      : this.all("SELECT * FROM inquiries WHERE tenant_id=? ORDER BY created_at", tenantId);
  }
  updateInquiryStatus(tenantId: string, id: string, status: string) {
    this.run("UPDATE inquiries SET status=? WHERE tenant_id=? AND id=?", status, tenantId, id);
  }

  // ---- CRM fixtures ----
  insertCustomer(c: { id: string; tenant_id: string; synthetic_name: string; synthetic_contact_ref: string }) {
    this.run("INSERT OR IGNORE INTO customers(id,tenant_id,synthetic_name,synthetic_contact_ref) VALUES(?,?,?,?)", c.id, c.tenant_id, c.synthetic_name, c.synthetic_contact_ref);
  }
  insertOrder(o: { id: string; tenant_id: string; customer_id: string; order_no: string; item: string; ordered_at: string; delivery_status: string }) {
    this.run("INSERT OR IGNORE INTO orders(id,tenant_id,customer_id,order_no,item,ordered_at,delivery_status) VALUES(?,?,?,?,?,?,?)", o.id, o.tenant_id, o.customer_id, o.order_no, o.item, o.ordered_at, o.delivery_status);
  }
  findOrderByNo(tenantId: string, orderNo: string) {
    return this.get<Row>("SELECT o.*, c.synthetic_name AS customer_name FROM orders o JOIN customers c ON c.id=o.customer_id WHERE o.tenant_id=? AND o.order_no=?", tenantId, orderNo);
  }
  countOrdersWithNoAnyTenant(orderNo: string): number {
    const r = this.get<{ n: number }>("SELECT COUNT(*) AS n FROM orders WHERE order_no=?", orderNo);
    return r?.n ?? 0;
  }
  listOrders(tenantId: string) { return this.all("SELECT * FROM orders WHERE tenant_id=? ORDER BY order_no", tenantId); }

  // ---- KB ----
  insertArticle(a: { id: string; tenant_id: string; title: string; intent_tags: string[]; body: string; version?: number; status?: string }) {
    this.run("INSERT OR REPLACE INTO kb_articles(id,tenant_id,title,intent_tags,body,version,status,updated_at) VALUES(?,?,?,?,?,?,?,?)",
      a.id, a.tenant_id, a.title, JSON.stringify(a.intent_tags), a.body, a.version ?? 1, a.status ?? "active", nowIso());
    // one chunk per article for the small fixture corpus
    this.run("INSERT OR REPLACE INTO kb_chunks(id,tenant_id,article_id,chunk_index,text,tokenized_text) VALUES(?,?,?,?,?,?)",
      `${a.id}-c0`, a.tenant_id, a.id, 0, a.body, "");
  }
  getArticle(tenantId: string, id: string) { return this.get<Row>("SELECT * FROM kb_articles WHERE tenant_id=? AND id=?", tenantId, id); }
  listArticles(tenantId: string) { return this.all("SELECT * FROM kb_articles WHERE tenant_id=? ORDER BY id", tenantId); }
  listActiveArticles(tenantId: string) { return this.all("SELECT * FROM kb_articles WHERE tenant_id=? AND status='active' ORDER BY id", tenantId); }
  updateArticle(tenantId: string, id: string, patch: { title?: string; body?: string; status?: string }) {
    const cur = this.getArticle(tenantId, id);
    if (!cur) throw new NotFoundError(`article ${id}`);
    this.run("UPDATE kb_articles SET title=?, body=?, status=?, version=version+1, updated_at=? WHERE tenant_id=? AND id=?",
      patch.title ?? cur.title, patch.body ?? cur.body, patch.status ?? cur.status, nowIso(), tenantId, id);
    this.run("UPDATE kb_chunks SET text=? WHERE tenant_id=? AND article_id=?", patch.body ?? cur.body, tenantId, id);
    return this.getArticle(tenantId, id)!;
  }

  // ---- runs & checkpoints ----
  createRun(r: { id: string; tenant_id: string; inquiry_id: string; idempotency_key?: string | null; prompt_version?: string; model_route?: string; trace_id?: string; config_snapshot?: unknown }) {
    if (r.idempotency_key) {
      const existing = this.get<Row>("SELECT * FROM runs WHERE idempotency_key=?", r.idempotency_key);
      if (existing) return existing;
    }
    this.run("INSERT INTO runs(id,tenant_id,inquiry_id,state,prompt_version,model_route,trace_id,config_snapshot,idempotency_key,created_at) VALUES(?,?,?,'queued',?,?,?,?,?,?)",
      r.id, r.tenant_id, r.inquiry_id, r.prompt_version ?? null, r.model_route ?? null, r.trace_id ?? null,
      r.config_snapshot ? JSON.stringify(r.config_snapshot) : null, r.idempotency_key ?? null, nowIso());
    return this.getRun(r.tenant_id, r.id)!;
  }
  getRun(tenantId: string, id: string) { return this.get<Row>("SELECT * FROM runs WHERE tenant_id=? AND id=?", tenantId, id); }
  getRunById(id: string) { return this.get<Row>("SELECT * FROM runs WHERE id=?", id); }
  listRunsForInquiry(tenantId: string, inquiryId: string) { return this.all("SELECT * FROM runs WHERE tenant_id=? AND inquiry_id=? ORDER BY created_at", tenantId, inquiryId); }
  claimNextRun(workerId: string, leaseMs: number) {
    return this.transaction(() => {
      const now = Date.now();
      const row = this.get<Row>(
        `SELECT * FROM runs WHERE state='queued' OR (state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?) ORDER BY created_at LIMIT 1`,
        new Date(now).toISOString());
      if (!row) return undefined;
      const expires = new Date(now + leaseMs).toISOString();
      this.run("UPDATE runs SET state='running', lease_owner=?, lease_expires_at=?, heartbeat_at=?, attempt_count=attempt_count+1, started_at=COALESCE(started_at,?) WHERE id=?",
        workerId, expires, nowIso(), nowIso(), row.id);
      return this.getRunById(row.id as string);
    });
  }
  heartbeatRun(id: string, leaseMs: number) {
    this.run("UPDATE runs SET heartbeat_at=?, lease_expires_at=? WHERE id=?", nowIso(), new Date(Date.now() + leaseMs).toISOString(), id);
  }
  finishRun(id: string, patch: { state: string; escalation_reason?: string | null; handoff_summary?: string | null; failure_code?: string | null; usage_json?: unknown; trace_id?: string }) {
    this.run("UPDATE runs SET state=?, escalation_reason=?, handoff_summary=?, last_error_code=?, usage_json=?, trace_id=COALESCE(?,trace_id), finished_at=?, lease_owner=NULL, lease_expires_at=NULL WHERE id=?",
      patch.state, patch.escalation_reason ?? null, patch.handoff_summary ?? null, patch.failure_code ?? null,
      patch.usage_json ? JSON.stringify(patch.usage_json) : null, patch.trace_id ?? null, nowIso(), id);
  }
  saveCheckpoint(c: { tenant_id: string; run_id: string; step_key: string; attempt: number; state: string; input_redacted?: unknown; output_redacted?: unknown; prompt_version?: string; model?: string; duration_ms?: number; trace_id?: string; span_id?: string; error_code?: string | null }) {
    this.run(`INSERT INTO run_steps(id,tenant_id,run_id,step_key,attempt,state,input_redacted,output_redacted,prompt_version,model,started_at,finished_at,duration_ms,trace_id,span_id,error_code)
      VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(run_id,step_key,attempt) DO UPDATE SET state=excluded.state, output_redacted=excluded.output_redacted, error_code=excluded.error_code, finished_at=excluded.finished_at, duration_ms=excluded.duration_ms`,
      newId("step"), c.tenant_id, c.run_id, c.step_key, c.attempt, c.state,
      c.input_redacted ? JSON.stringify(c.input_redacted) : null,
      c.output_redacted ? JSON.stringify(c.output_redacted) : null,
      c.prompt_version ?? null, c.model ?? null, nowIso(), nowIso(), c.duration_ms ?? null, c.trace_id ?? null, c.span_id ?? null, c.error_code ?? null);
  }
  getCheckpoints(tenantId: string, runId: string) { return this.all("SELECT * FROM run_steps WHERE tenant_id=? AND run_id=? ORDER BY rowid", tenantId, runId); }
  completedStepKeys(runId: string): Set<string> {
    const rows = this.all<{ step_key: string }>("SELECT DISTINCT step_key FROM run_steps WHERE run_id=? AND state='completed'", runId);
    return new Set(rows.map(r => r.step_key));
  }

  // ---- tool calls ----
  insertToolCall(t: { tenant_id: string; run_id?: string | null; server: string; tool: string; risk_level: string; args_redacted?: unknown; status: string; latency_ms?: number; trace_id?: string; span_id?: string }) {
    const id = newId("tool");
    this.run("INSERT INTO tool_calls(id,tenant_id,run_id,server,tool,risk_level,args_redacted,status,latency_ms,trace_id,span_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)",
      id, t.tenant_id, t.run_id ?? null, t.server, t.tool, t.risk_level,
      t.args_redacted ? JSON.stringify(t.args_redacted) : null, t.status, t.latency_ms ?? null, t.trace_id ?? null, t.span_id ?? null, nowIso());
    return id;
  }
  listToolCalls(tenantId: string, runId: string) { return this.all("SELECT * FROM tool_calls WHERE tenant_id=? AND run_id=? ORDER BY created_at", tenantId, runId); }

  // ---- drafts & actions ----
  insertDraft(d: { id: string; tenant_id: string; run_id: string; revision: number; body: string; cited_article_ids: string[]; factual_references: unknown; verification_json?: unknown; content_hash: string }) {
    this.run("INSERT INTO drafts(id,tenant_id,run_id,revision,body,cited_article_ids,factual_references,verification_json,content_hash,state,created_at) VALUES(?,?,?,?,?,?,?,?,?,'pending_review',?)",
      d.id, d.tenant_id, d.run_id, d.revision, d.body, JSON.stringify(d.cited_article_ids), JSON.stringify(d.factual_references),
      d.verification_json ? JSON.stringify(d.verification_json) : null, d.content_hash, nowIso());
    return this.getDraft(d.tenant_id, d.id)!;
  }
  getDraft(tenantId: string, id: string) { return this.get<Row>("SELECT * FROM drafts WHERE tenant_id=? AND id=?", tenantId, id); }
  getDraftByRun(tenantId: string, runId: string) { return this.get<Row>("SELECT * FROM drafts WHERE tenant_id=? AND run_id=? ORDER BY revision DESC LIMIT 1", tenantId, runId); }
  nextRevision(runId: string): number {
    const r = this.get<{ m: number | null }>("SELECT MAX(revision) AS m FROM drafts WHERE run_id=?", runId);
    return (r?.m ?? 0) + 1;
  }
  setDraftVerification(tenantId: string, id: string, verification: unknown) {
    this.run("UPDATE drafts SET verification_json=? WHERE tenant_id=? AND id=?", JSON.stringify(verification), tenantId, id);
  }
  setDraftState(tenantId: string, id: string, state: string) { this.run("UPDATE drafts SET state=? WHERE tenant_id=? AND id=?", state, tenantId, id); }
  supersedeOtherDrafts(tenantId: string, runId: string, keepId: string) {
    this.run("UPDATE drafts SET state='superseded' WHERE tenant_id=? AND run_id=? AND id<>? AND state='pending_review'", tenantId, runId, keepId);
  }
  insertProposedAction(a: { id: string; tenant_id: string; draft_id: string; run_id?: string | null; action_type: string; canonical_payload: unknown; payload_hash: string }) {
    this.run("INSERT INTO proposed_actions(id,tenant_id,draft_id,run_id,action_type,canonical_payload,payload_hash,state,created_at) VALUES(?,?,?,?,?,?,?,'proposed',?)",
      a.id, a.tenant_id, a.draft_id, a.run_id ?? null, a.action_type, JSON.stringify(a.canonical_payload), a.payload_hash, nowIso());
    return this.getAction(a.tenant_id, a.id)!;
  }
  getAction(tenantId: string, id: string) { return this.get<Row>("SELECT * FROM proposed_actions WHERE tenant_id=? AND id=?", tenantId, id); }
  getActionById(id: string) { return this.get<Row>("SELECT * FROM proposed_actions WHERE id=?", id); }
  listActionsByDraft(tenantId: string, draftId: string) { return this.all("SELECT * FROM proposed_actions WHERE tenant_id=? AND draft_id=? ORDER BY created_at", tenantId, draftId); }
  setActionState(id: string, state: string) { this.run("UPDATE proposed_actions SET state=? WHERE id=?", state, id); }
  /** Atomic single-use transition; returns true only for the caller that won. */
  transitionAction(id: string, from: string, to: string): boolean {
    const res = this.run("UPDATE proposed_actions SET state=? WHERE id=? AND state=?", to, id, from);
    return Number(res.changes) === 1;
  }

  // ---- approvals & execution jobs ----
  insertApproval(a: { id: string; tenant_id: string; draft_id: string; draft_revision: number; draft_hash: string; operator_id: string; selected_action_ids: string[]; policy_version: string; rejected_reason?: string | null }) {
    this.guard("approvals");
    this.run("INSERT INTO approvals(id,tenant_id,draft_id,draft_revision,draft_hash,operator_id,selected_action_ids,policy_version,approved_at,rejected_reason) VALUES(?,?,?,?,?,?,?,?,?,?)",
      a.id, a.tenant_id, a.draft_id, a.draft_revision, a.draft_hash, a.operator_id, JSON.stringify(a.selected_action_ids), a.policy_version, nowIso(), a.rejected_reason ?? null);
  }
  getApprovalForAction(actionId: string) {
    return this.get<Row>("SELECT * FROM approvals WHERE selected_action_ids LIKE ?", `%"${actionId}"%`);
  }
  insertExecutionJob(j: { id: string; tenant_id: string; proposed_action_id: string; idempotency_key: string }) {
    this.guard("execution_jobs");
    const existing = this.get<Row>("SELECT * FROM execution_jobs WHERE idempotency_key=?", j.idempotency_key);
    if (existing) return existing;
    this.run("INSERT INTO execution_jobs(id,tenant_id,proposed_action_id,idempotency_key,state,created_at) VALUES(?,?,?,?,'queued',?)",
      j.id, j.tenant_id, j.proposed_action_id, j.idempotency_key, nowIso());
    return this.get<Row>("SELECT * FROM execution_jobs WHERE id=?", j.id)!;
  }
  getJob(id: string) { return this.get<Row>("SELECT * FROM execution_jobs WHERE id=?", id); }
  getJobByAction(actionId: string) { return this.get<Row>("SELECT * FROM execution_jobs WHERE proposed_action_id=?", actionId); }
  claimNextJob(workerId: string, leaseMs: number) {
    return this.transaction(() => {
      const row = this.get<Row>(`SELECT * FROM execution_jobs WHERE state='queued' OR (state='running' AND lease_expires_at IS NOT NULL AND lease_expires_at < ?) ORDER BY created_at LIMIT 1`, nowIso());
      if (!row) return undefined;
      // A running job with an expired lease has an ambiguous outcome if it may
      // already have written: mark unknown instead of blindly re-running.
      if (row.state === "running") {
        this.run("UPDATE execution_jobs SET state='unknown', failure_code='LEASE_EXPIRED_AMBIGUOUS' WHERE id=?", row.id);
        return this.getJob(row.id as string);
      }
      this.run("UPDATE execution_jobs SET state='running', lease_owner=?, lease_expires_at=?, heartbeat_at=?, attempts=attempts+1 WHERE id=?",
        workerId, new Date(Date.now() + leaseMs).toISOString(), nowIso(), row.id);
      return this.getJob(row.id as string);
    });
  }
  finishJob(id: string, state: string, result?: unknown, failureCode?: string | null) {
    this.run("UPDATE execution_jobs SET state=?, result_json=?, failure_code=?, lease_owner=NULL, lease_expires_at=NULL WHERE id=?",
      state, result ? JSON.stringify(result) : null, failureCode ?? null, id);
  }

  // ---- fixture writes (executor only) ----
  insertTicket(t: { id: string; tenant_id: string; inquiry_id: string; priority?: string; source_action_id?: string }) {
    this.guard("tickets");
    this.run("INSERT INTO tickets(id,tenant_id,inquiry_id,priority,source_action_id,created_at) VALUES(?,?,?,?,?,?)",
      t.id, t.tenant_id, t.inquiry_id, t.priority ?? "normal", t.source_action_id ?? null, nowIso());
    return this.get<Row>("SELECT * FROM tickets WHERE id=?", t.id)!;
  }
  findTicketByAction(actionId: string) { return this.get<Row>("SELECT * FROM tickets WHERE source_action_id=?", actionId); }
  listTickets(tenantId: string) { return this.all("SELECT * FROM tickets WHERE tenant_id=? ORDER BY created_at", tenantId); }
  insertTicketNote(n: { id: string; tenant_id: string; ticket_id: string; body: string; source_action_id?: string }) {
    this.guard("ticket_notes");
    this.run("INSERT INTO ticket_notes(id,tenant_id,ticket_id,body,source_action_id,created_at) VALUES(?,?,?,?,?,?)",
      n.id, n.tenant_id, n.ticket_id, n.body, n.source_action_id ?? null, nowIso());
    return this.get<Row>("SELECT * FROM ticket_notes WHERE id=?", n.id)!;
  }
  findNoteByAction(actionId: string) { return this.get<Row>("SELECT * FROM ticket_notes WHERE source_action_id=?", actionId); }
  insertOutbox(o: { id: string; tenant_id: string; inquiry_id: string; draft_revision: number; body: string; source_action_id?: string }) {
    this.guard("outbox_messages");
    this.run("INSERT INTO outbox_messages(id,tenant_id,inquiry_id,draft_revision,body,source_action_id,created_at) VALUES(?,?,?,?,?,?,?)",
      o.id, o.tenant_id, o.inquiry_id, o.draft_revision, o.body, o.source_action_id ?? null, nowIso());
    return this.get<Row>("SELECT * FROM outbox_messages WHERE id=?", o.id)!;
  }
  findOutboxByAction(actionId: string) { return this.get<Row>("SELECT * FROM outbox_messages WHERE source_action_id=?", actionId); }
  listOutbox(tenantId: string) { return this.all("SELECT * FROM outbox_messages WHERE tenant_id=? ORDER BY created_at", tenantId); }
  countTickets(): number { return this.get<{ n: number }>("SELECT COUNT(*) AS n FROM tickets")?.n ?? 0; }
  countOutbox(): number { return this.get<{ n: number }>("SELECT COUNT(*) AS n FROM outbox_messages")?.n ?? 0; }

  // ---- audit & eval ----
  insertAudit(a: { tenant_id: string; actor_type: string; actor_id?: string | null; action: string; entity_type: string; entity_id: string; before_hash?: string | null; after_hash?: string | null; trace_id?: string | null }) {
    this.run("INSERT INTO audit_events(id,tenant_id,actor_type,actor_id,action,entity_type,entity_id,before_hash,after_hash,trace_id,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)",
      newId("audit"), a.tenant_id, a.actor_type, a.actor_id ?? null, a.action, a.entity_type, a.entity_id, a.before_hash ?? null, a.after_hash ?? null, a.trace_id ?? null, nowIso());
  }
  listAudit(tenantId: string, entityType?: string, entityId?: string) {
    if (entityType && entityId) return this.all("SELECT * FROM audit_events WHERE tenant_id=? AND entity_type=? AND entity_id=? ORDER BY created_at", tenantId, entityType, entityId);
    return this.all("SELECT * FROM audit_events WHERE tenant_id=? ORDER BY created_at DESC LIMIT 200", tenantId);
  }
  insertEvalRun(id: string, meta: { dataset_version?: string; prompt_version?: string; model?: string; index_version?: string }, report: unknown) {
    this.run("INSERT INTO eval_runs(id,dataset_version,prompt_version,model,index_version,report_json,created_at) VALUES(?,?,?,?,?,?,?)",
      id, meta.dataset_version ?? null, meta.prompt_version ?? null, meta.model ?? null, meta.index_version ?? null, JSON.stringify(report), nowIso());
  }
  listEvalRuns() { return this.all("SELECT id,dataset_version,prompt_version,model,index_version,created_at FROM eval_runs ORDER BY created_at DESC"); }
  getEvalRun(id: string) { return this.get<Row>("SELECT * FROM eval_runs WHERE id=?", id); }
}
