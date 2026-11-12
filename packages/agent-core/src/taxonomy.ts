export const INTENTS = ["delivery_status", "return_exchange", "invoice_request", "product_usage", "cancellation", "price_stock", "complaint", "account", "other"] as const;
export type Intent = (typeof INTENTS)[number];
export const INTENT_LABELS_JP: Record<Intent, string> = {
  delivery_status: "配送状況", return_exchange: "返品・交換", invoice_request: "請求書",
  product_usage: "使い方", cancellation: "解約・キャンセル", price_stock: "価格・在庫",
  complaint: "クレーム", account: "アカウント", other: "その他",
};
export const ESCALATION_REASONS = [
  "COMPLAINT_REQUIRES_HUMAN", "NO_RELEVANT_KB_EVIDENCE", "LOW_CLASSIFICATION_CONFIDENCE",
  "ORDER_ID_AMBIGUOUS", "POLICY_NOT_COVERED", "IDENTITY_SENSITIVE_REQUEST",
  "LEGAL_OR_SAFETY_SENSITIVE", "VERIFICATION_FAILED", "MODEL_BUDGET_EXCEEDED",
] as const;
export type EscalationReason = (typeof ESCALATION_REASONS)[number];

export const PROMPT_VERSION = "benri-prompt-v2.0.0";
export const POLICY_VERSION = "benri-policy-v1.0.0";

export interface BenriLimits {
  maxModelCallsPerRun: number; maxToolCallsPerRun: number; maxRunDurationMs: number;
  maxInputTokens: number; maxOutputTokens: number; maxCostPerRunJpy: number | null;
}
export function limitsFromEnv(env: NodeJS.ProcessEnv = process.env): BenriLimits {
  const num = (k: string, d: number) => { const v = Number(env[k]); return Number.isFinite(v) && v > 0 ? v : d; };
  return {
    maxModelCallsPerRun: num("BENRI_MAX_MODEL_CALLS_PER_RUN", 4),
    maxToolCallsPerRun: num("BENRI_MAX_TOOL_CALLS_PER_RUN", 8),
    maxRunDurationMs: num("BENRI_MAX_RUN_DURATION_MS", 30000),
    maxInputTokens: num("BENRI_MAX_INPUT_TOKENS", 8000),
    maxOutputTokens: num("BENRI_MAX_OUTPUT_TOKENS", 1200),
    maxCostPerRunJpy: env.BENRI_MAX_COST_PER_RUN_JPY ? Number(env.BENRI_MAX_COST_PER_RUN_JPY) : null,
  };
}
export class BenriError extends Error {
  constructor(public code: string, message: string) { super(message); this.name = "BenriError"; }
}
