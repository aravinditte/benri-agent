import { describe, expect, it } from "vitest";
import { BenriStore, contentHash, newId, seedInquiries, seedStore } from "@benri/db";
import { DirectToolGateway, approveDraft, mockLlmRespond, redactPII, runPipeline, limitsFromEnv } from "@benri/agent-core";
import { ModelRailClient } from "@benri/modelrail-client";
import { createTicketTool, TicketToolError } from "@benri/mcp-ticket";
import { DirectTicketGateway, OutcomeUnknownError, executeJob, type TicketGateway } from "./execute.js";

async function approvedSetup() {
  const full = new BenriStore(":memory:", "full");
  seedStore(full); seedInquiries(full, (t: string) => redactPII(t).text);
  const agent = full; // same in-memory DB; capability separation is tested in db tests
  const redacted = redactPII("請求書の発行をお願いできますか。").text;
  const inq = full.insertInquiry({ id: newId("inq"), tenant_id: "tenant-hinata", subject: "請求書", redacted_body: redacted, normalized_body_hash: contentHash(redacted), order_no_hint: "HIN-2026-0002" });
  const run = full.createRun({ id: newId("run"), tenant_id: "tenant-hinata", inquiry_id: String(inq.id), prompt_version: "v2", model_route: "mock" });
  full.claimNextRun("w", 60_000);
  const res = await runPipeline({ store: agent, runId: String(run.id), tools: new DirectToolGateway(full), llm: new ModelRailClient({}, mockLlmRespond), limits: limitsFromEnv() });
  if (res.outcome !== "awaiting_review") throw new Error(`setup pipeline outcome ${res.outcome}`);
  const draft = full.getDraftByRun("tenant-hinata", String(run.id))!;
  const actions = full.listActionsByDraft("tenant-hinata", String(draft.id));
  return { full, draft, actions, run };
}

describe("approval + executor (SPEC §6)", () => {
  it("approval enqueues jobs in one transaction; executor writes ticket + outbox exactly once", async () => {
    const { full, draft, actions } = await approvedSetup();
    const types = actions.map(a => String(a.action_type));
    expect(types).toContain("send_demo_reply");
    expect(types).toContain("create_ticket");
    const { execution_job_ids } = approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: Number(draft.revision), selectedActionIds: actions.map(a => String(a.id)), operatorId: "user-operator" });
    expect(execution_job_ids.length).toBe(actions.length);
    const gateway = new DirectTicketGateway(full);
    for (const jobId of execution_job_ids) {
      full.claimNextJob("exec", 60_000);
      const r = await executeJob(full, jobId, gateway);
      expect(r.state).toBe("succeeded");
    }
    expect(full.countTickets()).toBe(1);
    expect(full.countOutbox()).toBe(1);
    // Replay: re-executing finished jobs must not duplicate writes.
    for (const jobId of execution_job_ids) {
      const r = await executeJob(full, jobId, gateway);
      expect(r.state).toBe("succeeded");
    }
    expect(full.countTickets()).toBe(1);
    expect(full.countOutbox()).toBe(1);
  });

  it("re-approval of the same draft is rejected (no duplicate jobs)", async () => {
    const { full, draft, actions } = await approvedSetup();
    const sel = actions.map(a => String(a.id));
    approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: Number(draft.revision), selectedActionIds: sel, operatorId: "user-operator" });
    expect(() => approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: Number(draft.revision), selectedActionIds: sel, operatorId: "user-operator" })).toThrow();
  });

  it("approval with the wrong revision is rejected", async () => {
    const { full, draft, actions } = await approvedSetup();
    expect(() => approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: 99, selectedActionIds: actions.map(a => String(a.id)), operatorId: "user-operator" })).toThrowError(expect.objectContaining({ code: "REVISION_MISMATCH" }));
  });

  it("ticket tool refuses an action with no persisted approval", async () => {
    const { full, actions } = await approvedSetup();
    const ticketAction = actions.find(a => a.action_type === "create_ticket")!;
    expect(() => createTicketTool(full, { approved_action_id: String(ticketAction.id) })).toThrow(TicketToolError);
    expect(full.countTickets()).toBe(0);
  });

  it("partial approval executes only the selected actions", async () => {
    const { full, draft, actions } = await approvedSetup();
    const replyOnly = actions.filter(a => a.action_type === "send_demo_reply").map(a => String(a.id));
    const { execution_job_ids } = approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: Number(draft.revision), selectedActionIds: replyOnly, operatorId: "user-operator" });
    expect(execution_job_ids).toHaveLength(1);
    full.claimNextJob("exec", 60_000);
    await executeJob(full, execution_job_ids[0], new DirectTicketGateway(full));
    expect(full.countOutbox()).toBe(1);
    expect(full.countTickets()).toBe(0);
  });

  it("ambiguous gateway outcome marks the job unknown — never blind-retried", async () => {
    const { full, draft, actions } = await approvedSetup();
    const ticketAction = actions.find(a => a.action_type === "create_ticket")!;
    const { execution_job_ids } = approveDraft(full, { tenantId: "tenant-hinata", draftId: String(draft.id), revision: Number(draft.revision), selectedActionIds: [String(ticketAction.id)], operatorId: "user-operator" });
    full.claimNextJob("exec", 60_000);
    const unknownGateway: TicketGateway = {
      async createTicket() { throw new OutcomeUnknownError("timeout after send"); },
      async addTicketNote() { throw new OutcomeUnknownError("timeout after send"); },
    };
    const r = await executeJob(full, execution_job_ids[0], unknownGateway);
    expect(r.state).toBe("unknown");
    expect(String(full.getJob(execution_job_ids[0])!.state)).toBe("unknown");
  });
});
