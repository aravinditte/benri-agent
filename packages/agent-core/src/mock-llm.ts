// Deterministic MOCK LLM responder (SPEC §10.1: MOCK_LLM=1, case-keyed,
// clearly labelled). It implements the same structured contracts a live
// ModelRail model would: CLASSIFY returns JSON; DRAFT returns DraftOutput
// JSON with citations + factual references. It is NOT a live model result.
import { classifyText } from "./classify.js";
import { REQUIRED_SIGNATURE } from "@benri/japanese-quality";
import type { ChatMessage } from "@benri/modelrail-client";

export interface EvidenceItem { article_id: string; chunk_id: string; title: string; text: string }
export interface OrderContext { order_no_masked: string; item: string; delivery_status: string; ordered_at: string }
export interface FactualReference { factKey: string; normalizedValue: string; sourceArticleId: string; sourceChunkId: string }
export interface DraftOutput { body: string; citedArticleIds: string[]; factualReferences: FactualReference[] }

const VALUE_RE = /\d+(?:[.,]\d+)?(?:円|営業日|日以内|日|年間|年|か月|ヶ月|時間|%|％|点|件|回)|T\d{13}/g;

export function extractValues(text: string): string[] {
  return [...new Set((text.normalize("NFKC").match(VALUE_RE) ?? []))];
}

import { tokenize } from "@benri/retrieval";
function pickSentences(text: string, max: number, query?: string, minOverlap = 1): string[] {
  const sentences = text.split(/(?<=。)/).map(s => s.trim()).filter(Boolean);
  if (!query) return sentences.slice(0, max);
  const qTokens = new Set(tokenize(query).filter(t => t.length >= 2 && /[一-鿿ァ-ヶ]/.test(t)));
  const scored = sentences.map((s, i) => {
    const sTokens = new Set(tokenize(s));
    const overlap = [...qTokens].filter(t => sTokens.has(t)).length;
    const valueBonus = extractValues(s).length > 0 ? 0.5 : 0;
    return { s, i, score: overlap + valueBonus };
  }).filter(x => x.score >= minOverlap + 0.5 || (minOverlap <= 1 && x.score > 0.5));
  scored.sort((a, b) => b.score - a.score || a.i - b.i);
  const picked = scored.slice(0, max).sort((a, b) => a.i - b.i).map(x => x.s);
  return picked.length > 0 ? picked : (minOverlap <= 1 ? sentences.slice(0, 1) : []);
}

export function buildDraft(intent: string, evidence: EvidenceItem[], order: OrderContext | null, query?: string): DraftOutput {
  const lines: string[] = ["お問い合わせありがとうございます。ひなたストア カスタマーサポートです。"];
  const factualReferences: FactualReference[] = [];
  const cited: string[] = [];
  if (order) {
    lines.push(`ご注文の商品「${order.item}」につきまして、現在の配送状況は「${order.delivery_status}」となっております。`);
    factualReferences.push({ factKey: "delivery_status", normalizedValue: order.delivery_status, sourceArticleId: "CRM_ORDER", sourceChunkId: order.order_no_masked });
    factualReferences.push({ factKey: "item", normalizedValue: order.item, sourceArticleId: "CRM_ORDER", sourceChunkId: order.order_no_masked });
  }
  evidence.slice(0, 2).forEach((ev, idx) => {
    const sentences = pickSentences(ev.text, idx === 0 ? 3 : 2, query, idx === 0 ? 1 : 2);
    if (sentences.length === 0) return;
    cited.push(ev.article_id);
    for (const sentence of sentences) {
      lines.push(sentence);
      for (const v of extractValues(sentence)) {
        factualReferences.push({ factKey: "policy_value", normalizedValue: v, sourceArticleId: ev.article_id, sourceChunkId: ev.chunk_id });
      }
    }
  });
  lines.push("ご不明な点がございましたら、お気軽にお問い合わせください。");
  lines.push(REQUIRED_SIGNATURE);
  return { body: lines.join("\n"), citedArticleIds: [...new Set(cited)], factualReferences };
}

export function mockLlmRespond(messages: ChatMessage[]): string {
  const system = messages.find(m => m.role === "system")?.content ?? "";
  const user = messages.find(m => m.role === "user")?.content ?? "";
  if (system.includes("TASK:CLASSIFY")) {
    const payload = JSON.parse(user) as { text: string };
    return JSON.stringify(classifyText(payload.text));
  }
  if (system.includes("TASK:DRAFT")) {
    const payload = JSON.parse(user) as { intent: string; evidence: EvidenceItem[]; order: OrderContext | null; query?: string };
    return JSON.stringify(buildDraft(payload.intent, payload.evidence, payload.order, payload.query));
  }
  return JSON.stringify({ error: "unknown task" });
}
