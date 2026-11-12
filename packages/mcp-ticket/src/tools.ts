// Ticket MCP tool implementations (SPEC §7): write-only, executor host only.
// The tool takes ONLY an approved_action_id. It loads the canonical payload
// from the database server-side, verifies approval + payload hash, enforces
// single-use atomic transitions, and is idempotent: a repeated call for an
// already-succeeded action returns the existing fixture record, never a copy.
import { BenriStore, contentHash, newId } from "@benri/db";

export class TicketToolError extends Error {
  constructor(public code: string, message: string) { super(message); this.name = "TicketToolError"; }
}

function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  const obj = value as Record<string, unknown>;
  return `{${Object.keys(obj).sort().map(k => `${JSON.stringify(k)}:${stableStringify(obj[k])}`).join(",")}}`;
}

export function loadVerifiedAction(store: BenriStore, approvedActionId: string, expectedType: string) {
  const action = store.getActionById(approvedActionId);
  if (!action) throw new TicketToolError("ACTION_NOT_FOUND", `action ${approvedActionId}`);
  if (String(action.action_type) !== expectedType) throw new TicketToolError("ACTION_TYPE_MISMATCH", `expected ${expectedType}, got ${action.action_type}`);
  const approval = store.getApprovalForAction(approvedActionId);
  if (!approval) throw new TicketToolError("ACTION_NOT_APPROVED", "no persisted approval covers this action");
  const payload = JSON.parse(String(action.canonical_payload)) as Record<string, unknown>;
  if (contentHash(stableStringify(payload)) !== String(action.payload_hash)) throw new TicketToolError("PAYLOAD_HASH_MISMATCH", "stored payload does not match its hash");
  if (String(payload.tenant_id) !== String(action.tenant_id)) throw new TicketToolError("TENANT_MISMATCH", "payload tenant mismatch");
  return { action, payload };
}

export function createTicketTool(store: BenriStore, input: { approved_action_id: string }) {
  const existing = store.findTicketByAction(input.approved_action_id);
  if (existing) return { ticket_id: String(existing.id), idempotent_replay: true };
  const { action, payload } = loadVerifiedAction(store, input.approved_action_id, "create_ticket");
  if (action.state === "succeeded") throw new TicketToolError("ACTION_ALREADY_SUCCEEDED", "action succeeded but no ticket found — inconsistent state");
  if (!store.transitionAction(input.approved_action_id, "queued", "running")) {
    throw new TicketToolError("ACTION_NOT_QUEUED", `action state is ${action.state}`);
  }
  const args = payload.arguments as { priority?: string };
  const ticket = store.insertTicket({
    id: newId("ticket"), tenant_id: String(action.tenant_id),
    inquiry_id: String(payload.inquiry_id), priority: args.priority ?? "normal",
    source_action_id: input.approved_action_id,
  });
  store.setActionState(input.approved_action_id, "succeeded");
  store.insertAudit({ tenant_id: String(action.tenant_id), actor_type: "executor", actor_id: "executor-worker", action: "ticket.created", entity_type: "ticket", entity_id: String(ticket.id), after_hash: String(action.payload_hash) });
  return { ticket_id: String(ticket.id), idempotent_replay: false };
}

export function addTicketNoteTool(store: BenriStore, input: { approved_action_id: string }) {
  const existing = store.findNoteByAction(input.approved_action_id);
  if (existing) return { ticket_id: String(existing.ticket_id), note_id: String(existing.id), idempotent_replay: true };
  const { action, payload } = loadVerifiedAction(store, input.approved_action_id, "add_ticket_note");
  if (!store.transitionAction(input.approved_action_id, "queued", "running")) {
    throw new TicketToolError("ACTION_NOT_QUEUED", `action state is ${action.state}`);
  }
  // Attach to the ticket created for the same inquiry (by the sibling
  // create_ticket action); if none exists yet, fail visibly — never guess.
  const tickets = store.listTickets(String(action.tenant_id)).filter(t => String(t.inquiry_id) === String(payload.inquiry_id));
  if (tickets.length === 0) throw new TicketToolError("TICKET_NOT_FOUND", "no ticket exists for this inquiry yet");
  const args = payload.arguments as { note?: string };
  const note = store.insertTicketNote({
    id: newId("note"), tenant_id: String(action.tenant_id), ticket_id: String(tickets[0].id),
    body: args.note ?? "", source_action_id: input.approved_action_id,
  });
  store.setActionState(input.approved_action_id, "succeeded");
  return { ticket_id: String(tickets[0].id), note_id: String(note.id), idempotent_replay: false };
}
