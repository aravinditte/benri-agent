// Proposed actions come from deterministic intent/policy templates ONLY
// (SPEC §5.1, §8.5). The model never proposes or parameterizes actions.
import { contentHash } from "@benri/db";
import type { Intent } from "./taxonomy.js";

export type ActionType = "send_demo_reply" | "create_ticket" | "add_ticket_note";

const TEMPLATES: Partial<Record<Intent, ActionType[]>> = {
  delivery_status: ["send_demo_reply"],
  product_usage: ["send_demo_reply"],
  price_stock: ["send_demo_reply"],
  account: ["send_demo_reply"],
  other: ["send_demo_reply"],
  invoice_request: ["send_demo_reply", "create_ticket"],
  return_exchange: ["send_demo_reply", "create_ticket", "add_ticket_note"],
  cancellation: ["send_demo_reply", "create_ticket"],
};

export function actionTypesForIntent(intent: Intent): ActionType[] {
  return TEMPLATES[intent] ?? ["send_demo_reply"];
}

export function stableStringify(value: unknown): string {
  if (value === null || typeof value !== "object") return JSON.stringify(value) ?? "null";
  if (Array.isArray(value)) return `[${value.map(stableStringify).join(",")}]`;
  const obj = value as Record<string, unknown>;
  return `{${Object.keys(obj).sort().map(k => `${JSON.stringify(k)}:${stableStringify(obj[k])}`).join(",")}}`;
}

export interface CanonicalActionPayload {
  tenant_id: string; inquiry_id: string; draft_id: string; draft_revision: number;
  action_type: ActionType; target: string; arguments: Record<string, unknown>;
}

export function buildCanonicalPayload(input: {
  tenant_id: string; inquiry_id: string; draft_id: string; draft_revision: number;
  action_type: ActionType; draft_body: string; subject: string; urgency: string;
}): CanonicalActionPayload {
  const base = { tenant_id: input.tenant_id, inquiry_id: input.inquiry_id, draft_id: input.draft_id, draft_revision: input.draft_revision, action_type: input.action_type };
  switch (input.action_type) {
    case "send_demo_reply":
      return { ...base, target: "demo_outbox", arguments: { body: input.draft_body, draft_revision: input.draft_revision } };
    case "create_ticket":
      return { ...base, target: "demo_tickets", arguments: { subject: input.subject, priority: input.urgency === "high" ? "high" : "normal" } };
    case "add_ticket_note":
      return { ...base, target: "demo_tickets", arguments: { note: "オペレーター確認依頼: 承認済み対応内容に沿ってご対応をお願いいたします。" } };
  }
}
export function payloadHash(payload: CanonicalActionPayload): string {
  return contentHash(stableStringify(payload));
}
