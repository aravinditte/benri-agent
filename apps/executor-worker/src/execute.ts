// Executor (SPEC §6.2): accepts ONLY persisted, approved execution job IDs.
// Reloads the action + approval from the DB, verifies payload hashes, and
// performs each approved fixture action exactly once. send_demo_reply is an
// executor-internal outbox adapter — NOT an MCP tool (SPEC §7).
import { BenriStore, contentHash, newId } from "@benri/db";
import { createTicketTool, addTicketNoteTool, TicketToolError, loadVerifiedAction } from "@benri/mcp-ticket";

export interface TicketGateway {
  createTicket(approvedActionId: string): Promise<{ ticket_id: string; idempotent_replay: boolean }>;
  addTicketNote(approvedActionId: string): Promise<{ ticket_id: string; note_id: string; idempotent_replay: boolean }>;
}
export class DirectTicketGateway implements TicketGateway {
  constructor(private store: BenriStore) {}
  async createTicket(id: string) { return createTicketTool(this.store, { approved_action_id: id }); }
  async addTicketNote(id: string) { return addTicketNoteTool(this.store, { approved_action_id: id }); }
}
export class OutcomeUnknownError extends Error { code = "OUTCOME_UNKNOWN"; }

export interface ExecuteResult { job_id: string; state: "succeeded" | "failed" | "unknown"; result?: unknown; failure_code?: string }

export async function executeJob(store: BenriStore, jobId: string, gateway: TicketGateway): Promise<ExecuteResult> {
  const job = store.getJob(jobId);
  if (!job) return { job_id: jobId, state: "failed", failure_code: "JOB_NOT_FOUND" };
  if (job.state === "succeeded") return { job_id: jobId, state: "succeeded", result: job.result_json ? JSON.parse(String(job.result_json)) : undefined };
  if (job.state !== "running") return { job_id: jobId, state: "failed", failure_code: `JOB_NOT_CLAIMED:${job.state}` };
  const actionId = String(job.proposed_action_id);
  const action = store.getActionById(actionId);
  if (!action) { store.finishJob(jobId, "failed", undefined, "ACTION_NOT_FOUND"); return { job_id: jobId, state: "failed", failure_code: "ACTION_NOT_FOUND" }; }
  const tenantId = String(action.tenant_id);
  try {
    const type = String(action.action_type);
    if (type === "send_demo_reply") {
      const { payload } = loadVerifiedAction(store, actionId, "send_demo_reply");
      const existing = store.findOutboxByAction(actionId);
      if (existing) { store.finishJob(jobId, "succeeded", { outbox_id: String(existing.id), idempotent_replay: true }); return { job_id: jobId, state: "succeeded", result: { outbox_id: String(existing.id), idempotent_replay: true } }; }
      if (!store.transitionAction(actionId, "queued", "running")) throw new TicketToolError("ACTION_NOT_QUEUED", `action state is ${action.state}`);
      const args = payload.arguments as { body: string; draft_revision: number };
      const msg = store.insertOutbox({ id: newId("outbox"), tenant_id: tenantId, inquiry_id: String(payload.inquiry_id), draft_revision: Number(args.draft_revision), body: args.body, source_action_id: actionId });
      store.setActionState(actionId, "succeeded");
      store.insertAudit({ tenant_id: tenantId, actor_type: "executor", actor_id: "executor-worker", action: "outbox.demo_reply_written", entity_type: "outbox_messages", entity_id: String(msg.id), after_hash: String(action.payload_hash) });
      const result = { outbox_id: String(msg.id), delivery_state: "sent_demo", note: "Sent in demo outbox — not delivered" };
      store.finishJob(jobId, "succeeded", result);
      return { job_id: jobId, state: "succeeded", result };
    }
    if (type === "create_ticket" || type === "add_ticket_note") {
      const result = type === "create_ticket" ? await gateway.createTicket(actionId) : await gateway.addTicketNote(actionId);
      store.finishJob(jobId, "succeeded", result);
      return { job_id: jobId, state: "succeeded", result };
    }
    store.finishJob(jobId, "failed", undefined, `UNKNOWN_ACTION_TYPE:${type}`);
    return { job_id: jobId, state: "failed", failure_code: "UNKNOWN_ACTION_TYPE" };
  } catch (e) {
    if (e instanceof OutcomeUnknownError) {
      // Ambiguous fixture-write outcome: mark unknown, NEVER blind-retry (§3.4).
      store.finishJob(jobId, "unknown", undefined, "OUTCOME_UNKNOWN");
      store.insertAudit({ tenant_id: tenantId, actor_type: "executor", actor_id: "executor-worker", action: "execution.unknown", entity_type: "execution_jobs", entity_id: jobId });
      return { job_id: jobId, state: "unknown", failure_code: "OUTCOME_UNKNOWN" };
    }
    const code = e instanceof TicketToolError ? e.code : ((e as { code?: string }).code ?? "EXECUTION_ERROR");
    // If the action already succeeded (e.g. duplicate delivery of the job),
    // the job is a replay: succeed without writing again.
    const fresh = store.getActionById(actionId);
    if (fresh && fresh.state === "succeeded") { store.finishJob(jobId, "succeeded", { idempotent_replay: true }); return { job_id: jobId, state: "succeeded", result: { idempotent_replay: true } }; }
    store.finishJob(jobId, "failed", undefined, code);
    return { job_id: jobId, state: "failed", failure_code: code };
  }
}
export { contentHash };
