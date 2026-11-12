// Approval transaction (SPEC §6.1) — used by the API with an "api"-role store.
// Approval binds to the EXACT draft revision + explicitly selected actions;
// approval + execution jobs are persisted in ONE transaction.
import { BenriStore, contentHash, newId } from "@benri/db";
import { verifyDraft } from "./verifier.js";
import { payloadHash, stableStringify } from "./actions.js";
import { POLICY_VERSION } from "./taxonomy.js";
import type { EvidenceItem } from "./mock-llm.js";

export class ApprovalError extends Error {
  constructor(public code: string, message: string) { super(message); this.name = "ApprovalError"; }
}

export function approveDraft(store: BenriStore, input: {
  tenantId: string; draftId: string; revision: number; selectedActionIds: string[]; operatorId: string;
}): { approval_id: string; execution_job_ids: string[] } {
  const draft = store.getDraft(input.tenantId, input.draftId);
  if (!draft) throw new ApprovalError("DRAFT_NOT_FOUND", "draft not found");
  if (Number(draft.revision) !== input.revision) throw new ApprovalError("REVISION_MISMATCH", "approval must name the exact current draft revision");
  if (draft.state !== "pending_review") throw new ApprovalError("DRAFT_NOT_PENDING", `draft state is ${draft.state}`);
  if (contentHash(String(draft.body)) !== String(draft.content_hash)) throw new ApprovalError("DRAFT_HASH_MISMATCH", "draft body does not match its stored content hash");
  // Re-verify the exact revision before approving (§6.1 step 3).
  const run = store.getRun(input.tenantId, String(draft.run_id));
  const evidence: EvidenceItem[] = (JSON.parse(String(draft.cited_article_ids)) as string[]).map(id => {
    const art = store.getArticle(input.tenantId, id);
    return { article_id: id, chunk_id: `${id}-c0`, title: art ? String(art.title) : "", text: art ? String(art.body) : "" };
  });
  const articleTenant = new Map<string, string>();
  for (const a of store.listArticles(input.tenantId)) articleTenant.set(String(a.id), input.tenantId);
  // Reconstruct the order context from the run's lookup_order checkpoint so
  // CRM-sourced factual references are genuinely re-verified (§6.1 step 3).
  let order: import("./mock-llm.js").OrderContext | null = null;
  if (run) {
    for (const cp of store.getCheckpoints(input.tenantId, String(draft.run_id))) {
      if (cp.step_key === "lookup_order" && cp.state === "completed" && cp.output_redacted) {
        order = (JSON.parse(String(cp.output_redacted)) as { order: import("./mock-llm.js").OrderContext | null }).order;
      }
    }
  }
  const verification = verifyDraft(
    { body: String(draft.body), citedArticleIds: JSON.parse(String(draft.cited_article_ids)), factualReferences: JSON.parse(String(draft.factual_references)) },
    { tenantId: input.tenantId, evidence, articleTenant, order, orderText: order ? `${order.item} ${order.delivery_status}` : "" },
  );
  if (!verification.passed) {
    throw new ApprovalError("VERIFICATION_FAILED", "draft revision failed re-verification");
  }
  const actions = input.selectedActionIds.map(id => {
    const a = store.getAction(input.tenantId, id);
    if (!a) throw new ApprovalError("ACTION_NOT_FOUND", `action ${id}`);
    if (String(a.draft_id) !== input.draftId) throw new ApprovalError("ACTION_DRAFT_MISMATCH", "action belongs to a different draft");
    if (a.state !== "proposed") throw new ApprovalError("ACTION_NOT_PROPOSED", `action state is ${a.state}`);
    const payload = JSON.parse(String(a.canonical_payload));
    if (payloadHash(payload) !== String(a.payload_hash)) throw new ApprovalError("ACTION_HASH_MISMATCH", "action payload hash mismatch");
    if (Number(payload.draft_revision) !== input.revision) throw new ApprovalError("ACTION_REVISION_MISMATCH", "action was proposed for a different draft revision");
    return a;
  });
  if (actions.length === 0) throw new ApprovalError("NO_ACTIONS_SELECTED", "select at least one action");

  const approvalId = newId("approval");
  const jobIds: string[] = [];
  store.transaction(() => {
    store.setDraftState(input.tenantId, input.draftId, "approved");
    store.insertApproval({
      id: approvalId, tenant_id: input.tenantId, draft_id: input.draftId, draft_revision: input.revision,
      draft_hash: String(draft.content_hash), operator_id: input.operatorId,
      selected_action_ids: input.selectedActionIds, policy_version: POLICY_VERSION,
    });
    for (const a of actions) {
      store.setActionState(String(a.id), "approved");
      const job = store.insertExecutionJob({
        id: newId("job"), tenant_id: input.tenantId, proposed_action_id: String(a.id),
        idempotency_key: `exec:${String(a.id)}:${String(a.payload_hash)}`,
      });
      store.setActionState(String(a.id), "queued");
      jobIds.push(String(job.id));
    }
    store.insertAudit({
      tenant_id: input.tenantId, actor_type: "user", actor_id: input.operatorId, action: "draft.approved",
      entity_type: "draft", entity_id: input.draftId, before_hash: String(draft.content_hash), after_hash: contentHash(stableStringify(input.selectedActionIds)),
      trace_id: run ? String(run.trace_id ?? "") : null,
    });
  });
  return { approval_id: approvalId, execution_job_ids: jobIds };
}

export function rejectDraft(store: BenriStore, input: { tenantId: string; draftId: string; operatorId: string; reason: string }) {
  const draft = store.getDraft(input.tenantId, input.draftId);
  if (!draft) throw new ApprovalError("DRAFT_NOT_FOUND", "draft not found");
  store.transaction(() => {
    store.setDraftState(input.tenantId, input.draftId, "rejected");
    for (const a of store.listActionsByDraft(input.tenantId, input.draftId)) {
      if (a.state === "proposed") store.setActionState(String(a.id), "rejected");
    }
    store.insertApproval({
      id: newId("approval"), tenant_id: input.tenantId, draft_id: input.draftId, draft_revision: Number(draft.revision),
      draft_hash: String(draft.content_hash), operator_id: input.operatorId, selected_action_ids: [],
      policy_version: POLICY_VERSION, rejected_reason: input.reason,
    });
    store.insertAudit({ tenant_id: input.tenantId, actor_type: "user", actor_id: input.operatorId, action: "draft.rejected", entity_type: "draft", entity_id: input.draftId });
  });
}
