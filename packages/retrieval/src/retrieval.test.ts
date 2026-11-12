import { describe, expect, it } from "vitest";
import { LexicalIndex, tokenize, normalizeText } from "./index.js";

const docs = [
  { article_id: "KB-001", chunk_id: "KB-001-c0", title: "配送について", text: "配送状況はマイページでご確認いただけます。通常、ご注文から2〜3営業日で発送いたします。送料は550円です。配送の目安は地域により異なります。", intent_tags: ["delivery_status"] },
  { article_id: "KB-002", chunk_id: "KB-002-c0", title: "返品について", text: "返品は商品到着後30日以内であれば承ります。返品手続きはマイページからお申し込みください。交換も同じ期間で対応いたします。", intent_tags: ["return_exchange"] },
];
describe("lexical retrieval", () => {
  it("NFKC normalizes full-width digits", () => { expect(normalizeText("３０日")).toBe("30日"); });
  it("tokenizes Japanese into bigrams", () => { expect(tokenize("配送状況")).toContain("配送"); });
  it("ranks the delivery article first for a delivery query", () => {
    const idx = new LexicalIndex(docs);
    const hits = idx.search("配送はどれくらいかかりますか");
    expect(hits[0]?.article_id).toBe("KB-001");
  });
  it("ranks the returns article first for a returns query", () => {
    const idx = new LexicalIndex(docs);
    expect(idx.search("返品したいのですが")[0]?.article_id).toBe("KB-002");
  });
  it("returns no hits for an unrelated query", () => {
    const idx = new LexicalIndex(docs);
    expect(idx.search("ペット保険の補償内容")).toHaveLength(0);
  });
});
