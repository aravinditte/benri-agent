// Deterministic policy engine (SPEC §5.1 step 4). Runs BEFORE drafting.
// Hard rules only; no model involvement.
import type { Classification } from "./classify.js";
import { COMPLAINT_KEYWORDS, CREDENTIAL_DISCLOSURE_PATTERNS, IDENTITY_KEYWORDS, INJECTION_PATTERNS, LEGAL_SAFETY_KEYWORDS, PII_FISHING_PATTERNS, UNSUPPORTED_REQUEST_PATTERNS } from "./classify.js";
import type { EscalationReason } from "./taxonomy.js";

export interface PolicyInput {
  text: string;
  classification: Classification;
  evidenceCount: number;
  orderRequired: boolean;
  orderFound: boolean;
  orderHintPresent: boolean;
  orderAmbiguous?: boolean;
  evidenceTexts?: string[];
}
export interface PolicyDecision { escalate: boolean; reason?: EscalationReason; note_jp?: string }

const ORDER_REQUIRED_INTENTS = new Set(["delivery_status", "cancellation", "return_exchange"]);

export function evaluatePolicy(input: PolicyInput): PolicyDecision {
  const text = input.text.normalize("NFKC");
  const has = (list: string[]) => list.some(k => text.includes(k));
  if (has(LEGAL_SAFETY_KEYWORDS)) return { escalate: true, reason: "LEGAL_OR_SAFETY_SENSITIVE", note_jp: "法務・安全に関わる可能性があるため、担当者へ引き継ぎます。" };
  if (has(IDENTITY_KEYWORDS) || has(PII_FISHING_PATTERNS) || has(CREDENTIAL_DISCLOSURE_PATTERNS)) return { escalate: true, reason: "IDENTITY_SENSITIVE_REQUEST", note_jp: "本人確認・個人情報に関わる依頼のため、担当者へ引き継ぎます。" };
  if (input.classification.intent === "complaint" || has(COMPLAINT_KEYWORDS)) return { escalate: true, reason: "COMPLAINT_REQUIRES_HUMAN", note_jp: "クレーム対応は担当者が直接対応します。顧客向け返信案は作成しません。" };
  if (has(INJECTION_PATTERNS) || has(UNSUPPORTED_REQUEST_PATTERNS)) return { escalate: true, reason: "POLICY_NOT_COVERED", note_jp: "ポリシーで定められていない要求が含まれているため、担当者へ引き継ぎます。" };
  // Out-of-scope topics: the inquiry names a scope the KB defines away
  // (e.g. 海外配送 — the KB covers 日本全国 domestic delivery only) and no
  // retrieved evidence mentions it -> there is no grounded answer.
  if (text.includes("海外") && !(input.evidenceTexts ?? []).some(t => t.includes("海外"))) {
    return { escalate: true, reason: "NO_RELEVANT_KB_EVIDENCE", note_jp: "海外配送など、対応範囲外の内容については根拠となるナレッジがありません。" };
  }
  if (input.evidenceCount === 0) return { escalate: true, reason: "NO_RELEVANT_KB_EVIDENCE", note_jp: "回答の根拠となるナレッジが見つかりませんでした。" };
  if (input.orderAmbiguous) {
    return { escalate: true, reason: "ORDER_ID_AMBIGUOUS", note_jp: "複数のご注文番号が示されるなど、対象の注文を特定できませんでした。" };
  }
  if (input.orderHintPresent && ORDER_REQUIRED_INTENTS.has(input.classification.intent) && !input.orderFound) {
    return { escalate: true, reason: "ORDER_ID_AMBIGUOUS", note_jp: "ご注文番号に一致する注文が確認できませんでした。" };
  }
  if (input.classification.confidence < 0.6) return { escalate: true, reason: "LOW_CLASSIFICATION_CONFIDENCE", note_jp: "意図を十分な確度で判定できませんでした。" };
  return { escalate: false };
}
export { ORDER_REQUIRED_INTENTS };
