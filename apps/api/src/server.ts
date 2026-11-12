// Benri API (SPEC §12.1). Fastify, session-token auth (hashed at rest),
// tenant-scoped everything, stable error codes, idempotency keys on mutations.
import Fastify, { type FastifyInstance, type FastifyRequest } from "fastify";
import { randomBytes } from "node:crypto";
import { BenriStore, contentHash, hashToken, newId, verifyPassword } from "@benri/db";
import { approveDraft, rejectDraft, ApprovalError, redactPII, verifyDraft, limitsFromEnv } from "@benri/agent-core";
import type { EvidenceItem } from "@benri/agent-core";

export interface ApiOptions { dbPath: string; seedIfEmpty?: boolean }

interface AuthedRequest extends FastifyRequest { user?: { id: string; tenant_id: string; email: string; role: string } }

const err = (code: string, message: string) => ({ error: { code, message } });

export async function buildApi(opts: ApiOptions): Promise<{ app: FastifyInstance; store: BenriStore }> {
  const store = new BenriStore(opts.dbPath, "api");
  if (opts.seedIfEmpty) {
    const { seedStore, seedInquiries } = await import("@benri/db");
    if (!store.findUserByEmail("operator@hinata.example")) {
      seedStore(store);
      seedInquiries(store, (t: string) => redactPII(t).text);
    }
  }
  const app = Fastify({ logger: false });

  const auth = async (req: AuthedRequest) => {
    const header = req.headers.authorization;
    if (!header?.startsWith("Bearer ")) return null;
    const user = store.findUserBySession(hashToken(header.slice(7)));
    if (!user) return null;
    req.user = { id: String(user.id), tenant_id: String(user.tenant_id), email: String(user.email), role: String(user.role) };
    return req.user;
  };
  const requireAuth = async (req: AuthedRequest, reply: { code: (n: number) => { send: (b: unknown) => void } }) => {
    if (!(await auth(req))) { reply.code(401).send(err("UNAUTHORIZED", "login required")); return false; }
    return true;
  };

  app.get("/health", async () => ({ status: "ok", mode: process.env.MOCK_LLM === "0" ? "live" : "mock", llm_label: "MOCK_LLM — deterministic mock responses" }));

  app.post("/auth/login", async (req, reply) => {
    const { email, password } = (req.body ?? {}) as { email?: string; password?: string };
    const user = email ? store.findUserByEmail(email) : undefined;
    if (!user || !password || !verifyPassword(password, String(user.password_hash))) {
      return reply.code(401).send(err("INVALID_CREDENTIALS", "email or password incorrect"));
    }
    const token = randomBytes(32).toString("hex");
    store.createSession(hashToken(token), String(user.id));
    return { token, user: { id: String(user.id), email: String(user.email), role: String(user.role), tenant_id: String(user.tenant_id) } };
  });
  app.post("/auth/logout", async (req, reply) => {
    const header = req.headers.authorization;
    if (header?.startsWith("Bearer ")) store.deleteSession(hashToken(header.slice(7)));
    return { ok: true };
  });
  app.get("/auth/session", async (req: AuthedRequest, reply) => {
    const u = await auth(req);
    if (!u) return reply.code(401).send(err("UNAUTHORIZED", "login required"));
    return { user: u };
  });

  app.get("/inquiries", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const state = (req.query as { state?: string }).state;
    return { inquiries: store.listInquiries(req.user!.tenant_id, state) };
  });
  app.get("/inquiries/:id", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const { id } = req.params as { id: string };
    const inquiry = store.getInquiry(req.user!.tenant_id, id);
    if (!inquiry) return reply.code(404).send(err("NOT_FOUND", "inquiry not found"));
    return { inquiry, runs: store.listRunsForInquiry(req.user!.tenant_id, id) };
  });
  app.post("/inquiries", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    if (process.env.DEMO_MODE !== "1" && process.env.NODE_ENV === "production") return reply.code(403).send(err("FORBIDDEN", "arbitrary submission disabled in production demo"));
    const { subject, body, order_no_hint } = (req.body ?? {}) as { subject?: string; body?: string; order_no_hint?: string };
    if (!subject || !body) return reply.code(400).send(err("VALIDATION_ERROR", "subject and body required"));
    const redacted = redactPII(body).text; // redact BEFORE persistence (§5.1 step 0)
    const inquiry = store.insertInquiry({
      id: newId("inq"), tenant_id: req.user!.tenant_id, source: "local_dev", subject,
      redacted_body: redacted, normalized_body_hash: contentHash(redacted), order_no_hint: order_no_hint ?? null,
    });
    return reply.code(201).send({ inquiry });
  });
  app.post("/inquiries/:id/runs", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const { id } = req.params as { id: string };
    const inquiry = store.getInquiry(req.user!.tenant_id, id);
    if (!inquiry) return reply.code(404).send(err("NOT_FOUND", "inquiry not found"));
    const idem = (req.headers["idempotency-key"] as string) ?? `run:${req.user!.tenant_id}:${id}:${(req.body as { request_key?: string })?.request_key ?? "default"}`;
    const run = store.createRun({
      id: newId("run"), tenant_id: req.user!.tenant_id, inquiry_id: id, idempotency_key: idem,
      prompt_version: "benri-prompt-v2.0.0", model_route: process.env.MOCK_LLM === "0" ? "modelrail" : "mock-deterministic-v1",
      config_snapshot: limitsFromEnv(),
    });
    store.updateInquiryStatus(req.user!.tenant_id, id, "queued");
    return reply.code(201).send({ run_id: String(run.id), state: String(run.state) });
  });

  const runDetail = (tenantId: string, runId: string) => {
    const run = store.getRun(tenantId, runId);
    if (!run) return null;
    const draft = store.getDraftByRun(tenantId, runId);
    const evidence: Array<{ article_id: string; title: string; excerpt: string }> = [];
    if (draft) {
      for (const aid of JSON.parse(String(draft.cited_article_ids)) as string[]) {
        const art = store.getArticle(tenantId, aid);
        if (art) evidence.push({ article_id: aid, title: String(art.title), excerpt: String(art.body).slice(0, 240) });
      }
    }
    return {
      run, steps: store.getCheckpoints(tenantId, runId), tool_calls: store.listToolCalls(tenantId, runId),
      draft: draft ? { ...draft, verification: draft.verification_json ? JSON.parse(String(draft.verification_json)) : null } : null,
      proposed_actions: draft ? store.listActionsByDraft(tenantId, String(draft.id)) : [],
      escalation: run.escalation_reason ? { reason_code: String(run.escalation_reason), summary: String(run.handoff_summary ?? "") } : null,
      evidence,
    };
  };
  app.get("/runs/:id", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const detail = runDetail(req.user!.tenant_id, (req.params as { id: string }).id);
    if (!detail) return reply.code(404).send(err("NOT_FOUND", "run not found"));
    return detail;
  });

  app.get("/drafts/:id", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const draft = store.getDraft(req.user!.tenant_id, (req.params as { id: string }).id);
    if (!draft) return reply.code(404).send(err("NOT_FOUND", "draft not found"));
    return { draft, proposed_actions: store.listActionsByDraft(req.user!.tenant_id, String(draft.id)) };
  });
  app.post("/drafts/:id/revisions", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const { id } = req.params as { id: string };
    const { body } = (req.body ?? {}) as { body?: string };
    if (!body) return reply.code(400).send(err("VALIDATION_ERROR", "body required"));
    const current = store.getDraft(req.user!.tenant_id, id);
    if (!current) return reply.code(404).send(err("NOT_FOUND", "draft not found"));
    if (current.state !== "pending_review") return reply.code(409).send(err("DRAFT_NOT_PENDING", `draft state is ${current.state}`));
    // New immutable revision; prior verification/approval cannot be reused (§6.1).
    // Edited body keeps the original citations/factual refs only if they still
    // verify — verification is re-run against the run's evidence bundle.
    const runId = String(current.run_id);
    const hitsCp = store.getCheckpoints(req.user!.tenant_id, runId).find(c => c.step_key === "retrieve" && c.state === "completed");
    const hits = hitsCp?.output_redacted ? (JSON.parse(String(hitsCp.output_redacted)) as { hits: Array<{ article_id: string; chunk_id: string; title: string; excerpt: string }> }).hits : [];
    const evidence: EvidenceItem[] = hits.map(h => {
      const art = store.getArticle(req.user!.tenant_id, h.article_id);
      return { article_id: h.article_id, chunk_id: h.chunk_id, title: h.title, text: art ? String(art.body) : h.excerpt };
    });
    const articleTenant = new Map<string, string>();
    for (const a of store.listArticles(req.user!.tenant_id)) articleTenant.set(String(a.id), req.user!.tenant_id);
    let order: import("@benri/agent-core").OrderContext | null = null;
    for (const cp of store.getCheckpoints(req.user!.tenant_id, runId)) {
      if (cp.step_key === "lookup_order" && cp.state === "completed" && cp.output_redacted) order = (JSON.parse(String(cp.output_redacted)) as { order: import("@benri/agent-core").OrderContext | null }).order;
    }
    const cited = JSON.parse(String(current.cited_article_ids)) as string[];
    const refs = (JSON.parse(String(current.factual_references)) as Array<{ normalizedValue: string }>).filter(r => body.normalize("NFKC").includes(r.normalizedValue.normalize("NFKC")));
    const verification = verifyDraft({ body, citedArticleIds: cited, factualReferences: refs as never }, { tenantId: req.user!.tenant_id, evidence, articleTenant, order, orderText: order ? `${order.item} ${order.delivery_status}` : "" });
    const revision = store.nextRevision(runId);
    const newDraft = store.insertDraft({
      id: newId("draft"), tenant_id: req.user!.tenant_id, run_id: runId, revision, body,
      cited_article_ids: cited, factual_references: refs, verification_json: verification, content_hash: contentHash(body),
    });
    store.supersedeOtherDrafts(req.user!.tenant_id, runId, String(newDraft.id));
    // Deterministic action templates are re-issued for the new revision.
    const inquiry = store.getInquiry(req.user!.tenant_id, String(store.getRun(req.user!.tenant_id, runId)?.inquiry_id));
    const classificationCp = store.getCheckpoints(req.user!.tenant_id, runId).find(c => c.step_key === "classify" && c.state === "completed");
    const intent = classificationCp?.output_redacted ? (JSON.parse(String(classificationCp.output_redacted)) as { intent: string }).intent : "other";
    const { actionTypesForIntent, buildCanonicalPayload, payloadHash } = await import("@benri/agent-core");
    for (const actionType of actionTypesForIntent(intent as never)) {
      const payload = buildCanonicalPayload({ tenant_id: req.user!.tenant_id, inquiry_id: String(inquiry?.id ?? ""), draft_id: String(newDraft.id), draft_revision: revision, action_type: actionType, draft_body: body, subject: String(inquiry?.subject ?? ""), urgency: "normal" });
      store.insertProposedAction({ id: newId("action"), tenant_id: req.user!.tenant_id, draft_id: String(newDraft.id), run_id: runId, action_type: actionType, canonical_payload: payload, payload_hash: payloadHash(payload) });
    }
    store.insertAudit({ tenant_id: req.user!.tenant_id, actor_type: "user", actor_id: req.user!.id, action: "draft.revision_created", entity_type: "draft", entity_id: String(newDraft.id), before_hash: String(current.content_hash), after_hash: contentHash(body) });
    return reply.code(201).send({ draft: newDraft, verification });
  });
  app.post("/drafts/:id/approve", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const { id } = req.params as { id: string };
    const { revision, selected_action_ids } = (req.body ?? {}) as { revision?: number; selected_action_ids?: string[] };
    if (typeof revision !== "number" || !Array.isArray(selected_action_ids)) return reply.code(400).send(err("VALIDATION_ERROR", "revision and selected_action_ids required"));
    try {
      const result = approveDraft(store, { tenantId: req.user!.tenant_id, draftId: id, revision, selectedActionIds: selected_action_ids, operatorId: req.user!.id });
      return result;
    } catch (e) {
      if (e instanceof ApprovalError) return reply.code(409).send(err(e.code, e.message));
      throw e;
    }
  });
  app.post("/drafts/:id/reject", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const { id } = req.params as { id: string };
    const { reason } = (req.body ?? {}) as { reason?: string };
    if (!reason) return reply.code(400).send(err("VALIDATION_ERROR", "reason required"));
    try {
      rejectDraft(store, { tenantId: req.user!.tenant_id, draftId: id, operatorId: req.user!.id, reason });
      return { ok: true };
    } catch (e) {
      if (e instanceof ApprovalError) return reply.code(409).send(err(e.code, e.message));
      throw e;
    }
  });

  app.get("/tickets", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    return { tickets: store.listTickets(req.user!.tenant_id) };
  });
  app.get("/outbox", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    return { outbox: store.listOutbox(req.user!.tenant_id).map(m => ({ ...m, delivery_label: "Sent in demo outbox — not delivered" })) };
  });
  app.get("/kb/articles", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    return { articles: store.listArticles(req.user!.tenant_id) };
  });
  const requireAdmin = async (req: AuthedRequest, reply: { code: (n: number) => { send: (b: unknown) => void } }) => {
    if (!(await requireAuth(req, reply as never))) return false;
    if (req.user!.role !== "admin") { reply.code(403).send(err("FORBIDDEN", "admin only")); return false; }
    return true;
  };
  app.post("/kb/articles", async (req: AuthedRequest, reply) => {
    if (!(await requireAdmin(req, reply as never))) return;
    const b = (req.body ?? {}) as { id?: string; title?: string; body?: string; intent_tags?: string[] };
    if (!b.id || !b.title || !b.body) return reply.code(400).send(err("VALIDATION_ERROR", "id, title, body required"));
    const article = store.insertArticle({ id: b.id, tenant_id: req.user!.tenant_id, title: b.title, intent_tags: b.intent_tags ?? [], body: b.body });
    return reply.code(201).send({ article });
  });
  app.patch("/kb/articles/:id", async (req: AuthedRequest, reply) => {
    if (!(await requireAdmin(req, reply as never))) return;
    const { id } = req.params as { id: string };
    try {
      const article = store.updateArticle(req.user!.tenant_id, id, (req.body ?? {}) as { title?: string; body?: string; status?: string });
      return { article };
    } catch { return reply.code(404).send(err("NOT_FOUND", "article not found")); }
  });
  app.post("/kb/reindex", async (req: AuthedRequest, reply) => {
    if (!(await requireAdmin(req, reply as never))) return;
    // Lexical index is built in-memory per query from kb_chunks; a reindex is a
    // version bump recorded in audit. Index version: lexical-v1-bigram.
    store.insertAudit({ tenant_id: req.user!.tenant_id, actor_type: "user", actor_id: req.user!.id, action: "kb.reindex", entity_type: "kb", entity_id: "lexical-v1-bigram" });
    return { ok: true, index_version: "lexical-v1-bigram" };
  });
  app.get("/evals/runs", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    return { eval_runs: store.listEvalRuns() };
  });
  app.get("/evals/runs/:id", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const row = store.getEvalRun((req.params as { id: string }).id);
    if (!row) return reply.code(404).send(err("NOT_FOUND", "eval run not found"));
    return { eval_run: row, report: row.report_json ? JSON.parse(String(row.report_json)) : null };
  });
  app.get("/audit", async (req: AuthedRequest, reply) => {
    if (!(await requireAuth(req, reply as never))) return;
    const q = req.query as { entity_type?: string; entity_id?: string };
    return { audit_events: store.listAudit(req.user!.tenant_id, q.entity_type, q.entity_id) };
  });

  return { app, store };
}

async function main() {
  const port = Number(process.env.API_PORT ?? 4100);
  const { app } = await buildApi({ dbPath: process.env.BENRI_DB_PATH ?? "./data/benri.sqlite", seedIfEmpty: true });
  await app.listen({ port, host: "0.0.0.0" });
  console.log(`benri api listening on ${port} (MOCK_LLM=${process.env.MOCK_LLM ?? "1"})`);
}
if (import.meta.url === `file://${process.argv[1]}`) void main();
