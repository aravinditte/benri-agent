// Deterministic draft verifier (SPEC §8.5). Checks schema, citation
// existence/tenant/evidence membership, factual-reference value matching,
// policy-sensitive numbers/dates, signature and banned-phrase/style rules.
// LIMIT: this does NOT prove semantic entailment of free-form sentences.
// Human review remains mandatory.
import { z } from "zod";
import { checkJapaneseStyle } from "@benri/japanese-quality";
import { extractValues, type DraftOutput, type EvidenceItem, type OrderContext } from "./mock-llm.js";

export const DraftOutputSchema = z.object({
  body: z.string().min(1).max(4000),
  citedArticleIds: z.array(z.string()).min(1),
  factualReferences: z.array(z.object({
    factKey: z.string(), normalizedValue: z.string(),
    sourceArticleId: z.string(), sourceChunkId: z.string(),
  })),
});

export interface VerificationResult {
  passed: boolean;
  checks: Array<{ name: string; passed: boolean; detail?: string }>;
  verified_at: string;
  verifier_version: string;
}

export function verifyDraft(
  draft: DraftOutput,
  ctx: { tenantId: string; evidence: EvidenceItem[]; articleTenant: Map<string, string>; order: OrderContext | null; orderText: string },
): VerificationResult {
  const checks: VerificationResult["checks"] = [];
  const push = (name: string, passed: boolean, detail?: string) => checks.push({ name, passed, detail });

  const schema = DraftOutputSchema.safeParse(draft);
  push("schema", schema.success, schema.success ? undefined : "draft output failed schema validation");
  if (!schema.success) return { passed: false, checks, verified_at: new Date().toISOString(), verifier_version: "verifier-v1" };

  const evidenceIds = new Set(ctx.evidence.map(e => e.article_id));
  let citationsOk = true;
  for (const id of draft.citedArticleIds) {
    if (!evidenceIds.has(id)) { citationsOk = false; push("citation_membership", false, `${id} not in retrieved evidence`); }
    const tenant = ctx.articleTenant.get(id);
    if (tenant !== undefined && tenant !== ctx.tenantId) { citationsOk = false; push("citation_tenant", false, `${id} belongs to another tenant`); }
  }
  if (citationsOk) push("citation_membership_and_tenant", true);

  const sourceText = (articleId: string): string | null => {
    if (articleId === "CRM_ORDER") return ctx.orderText;
    return ctx.evidence.find(e => e.article_id === articleId)?.text ?? null;
  };
  let factsOk = true;
  for (const ref of draft.factualReferences) {
    const src = sourceText(ref.sourceArticleId);
    if (src === null) { factsOk = false; push("factual_reference_source", false, `${ref.factKey} source missing`); continue; }
    if (!src.normalize("NFKC").includes(ref.normalizedValue.normalize("NFKC"))) { factsOk = false; push("factual_reference_value", false, `${ref.factKey}=${ref.normalizedValue} not in source`); }
    if (!draft.body.normalize("NFKC").includes(ref.normalizedValue.normalize("NFKC"))) { factsOk = false; push("factual_reference_in_body", false, `${ref.factKey}=${ref.normalizedValue} not in draft body`); }
  }
  if (factsOk) push("factual_references", true);

  // Policy-sensitive values: every number/date-like value in the body must
  // be supported by evidence or the order record.
  const supported = new Set<string>();
  for (const e of ctx.evidence) for (const v of extractValues(e.text)) supported.add(v);
  if (ctx.order) { supported.add(ctx.order.delivery_status); supported.add(ctx.order.item); }
  const unsupported = extractValues(draft.body).filter(v => ![...supported].some(s => s === v));
  push("policy_sensitive_values", unsupported.length === 0, unsupported.length ? `unsupported: ${unsupported.join(",")}` : undefined);

  const style = checkJapaneseStyle(draft.body);
  push("japanese_style", style.ok, style.ok ? undefined : style.violations.join(","));

  return { passed: checks.every(c => c.passed), checks, verified_at: new Date().toISOString(), verifier_version: "verifier-v1" };
}
