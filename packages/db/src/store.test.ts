import { describe, expect, it } from "vitest";
import { BenriStore } from "./sqlite-store.js";
import { CapabilityError } from "./errors.js";

describe("capability-scoped store handles (SPEC §3.3/§6)", () => {
  it("agent handle structurally rejects ticket/outbox/approval writes", () => {
    const agent = new BenriStore(":memory:", "agent");
    expect(() => agent.insertTicket({ id: "t1", tenant_id: "tenant-hinata", inquiry_id: "i1" })).toThrow(CapabilityError);
    expect(() => agent.insertOutbox({ id: "o1", tenant_id: "tenant-hinata", inquiry_id: "i1", draft_revision: 1, body: "x" })).toThrow(CapabilityError);
    expect(() => agent.insertApproval({ id: "a1", tenant_id: "tenant-hinata", draft_id: "d1", draft_revision: 1, draft_hash: "h", operator_id: "u1", selected_action_ids: [], policy_version: "p1" })).toThrow(CapabilityError);
    expect(() => agent.insertExecutionJob({ id: "j1", tenant_id: "tenant-hinata", proposed_action_id: "pa1", idempotency_key: "k1" })).toThrow(CapabilityError);
  });
  it("api handle cannot write tickets directly; executor can", () => {
    const api = new BenriStore(":memory:", "api");
    expect(() => api.insertTicket({ id: "t1", tenant_id: "tenant-hinata", inquiry_id: "i1" })).toThrow(CapabilityError);
    const exec = new BenriStore(":memory:", "executor");
    exec.insertTicket({ id: "t1", tenant_id: "tenant-hinata", inquiry_id: "i1" });
    expect(exec.countTickets()).toBe(1);
  });
  it("cross-tenant reads fail closed (return undefined)", () => {
    const s = new BenriStore(":memory:", "full");
    s.insertTenant("tenant-hinata", "H"); s.insertTenant("tenant-sakura", "S");
    s.insertInquiry({ id: "inq-1", tenant_id: "tenant-hinata", subject: "s", redacted_body: "b", normalized_body_hash: "h" });
    expect(s.getInquiry("tenant-sakura", "inq-1")).toBeUndefined();
    expect(s.getInquiry("tenant-hinata", "inq-1")).toBeTruthy();
    s.insertArticle({ id: "KB-1", tenant_id: "tenant-hinata", title: "t", intent_tags: [], body: "b" });
    expect(s.getArticle("tenant-sakura", "KB-1")).toBeUndefined();
  });
  it("checkpoint uniqueness: same (run,step,attempt) upserts, new attempt adds a row", () => {
    const s = new BenriStore(":memory:", "full");
    const c = { tenant_id: "tenant-hinata", run_id: "r1", step_key: "classify", attempt: 1, state: "completed" };
    s.saveCheckpoint(c); s.saveCheckpoint(c);
    expect(s.getCheckpoints("tenant-hinata", "r1")).toHaveLength(1);
    s.saveCheckpoint({ ...c, attempt: 2 });
    expect(s.getCheckpoints("tenant-hinata", "r1")).toHaveLength(2);
    expect(s.completedStepKeys("r1").has("classify")).toBe(true);
  });
  it("execution job idempotency key is unique", () => {
    const s = new BenriStore(":memory:", "full");
    const j1 = s.insertExecutionJob({ id: "j1", tenant_id: "tenant-hinata", proposed_action_id: "pa1", idempotency_key: "k1" });
    const j2 = s.insertExecutionJob({ id: "j2", tenant_id: "tenant-hinata", proposed_action_id: "pa1", idempotency_key: "k1" });
    expect(j2.id).toBe(j1.id);
  });
});
