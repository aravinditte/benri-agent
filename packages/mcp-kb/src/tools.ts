// KB MCP tool implementations (SPEC §7): read-only search_kb / get_article.
// Pure functions over the store so they are testable without MCP transport.
import type { BenriStore } from "@benri/db";
import { LexicalIndex, type SearchHit } from "@benri/retrieval";

export function searchKbTool(store: BenriStore, input: { tenant_id: string; query: string; intent?: string; limit?: number }): SearchHit[] {
  const articles = store.listActiveArticles(input.tenant_id);
  const index = new LexicalIndex(articles.map(a => ({
    article_id: String(a.id), chunk_id: `${a.id}-c0`, title: String(a.title), text: String(a.body),
    intent_tags: JSON.parse(String(a.intent_tags)) as string[],
  })));
  return index.search(input.query, { intent: input.intent, limit: input.limit ?? 5 });
}

export function getArticleTool(store: BenriStore, input: { tenant_id: string; article_id: string }) {
  const a = store.getArticle(input.tenant_id, input.article_id);
  if (!a) return null;
  return { article_id: String(a.id), title: String(a.title), body: String(a.body), version: Number(a.version), status: String(a.status), intent_tags: JSON.parse(String(a.intent_tags)) };
}
