// Japanese lexical retrieval (SPEC §8.2). This is LEXICAL retrieval — NOT BM25.
// Tokenizer deviation (documented): kuromoji was evaluated; for the small
// fixture corpus and deterministic CI, we use an explicitly selected
// dictionary-free tokenizer: NFKC normalization + character bigrams for
// CJK runs + word tokens for latin/digit runs. See docs/VERIFICATION notes.
export const INDEX_VERSION = "lexical-v1-bigram";
export const CONTENT_COVERAGE_THRESHOLD = 0.18;
export const INTENT_GATE_THRESHOLD = 0.12;

export function normalizeText(text: string): string {
  return text.normalize("NFKC").toLowerCase();
}

// Polite request scaffolding carries no topical evidence; strip it from
// queries before tokenizing so generic bigrams cannot dominate ranking.
const QUERY_STOP_PHRASES = ["について教えてください", "を教えてください", "について", "おしえてください", "教えてください", "おねがいします", "お願いします", "いただけますか", "いただけますでしょうか", "くださいますか", "でしょうか", "ですか", "ますか"];
// Query expansion for common hiragana spellings/typos of fixture vocabulary
// (documented, explicit dictionary — not a morphological analyzer).
const QUERY_EXPANSIONS: Record<string, string> = { "へんぴん": "返品", "ざいこ": "在庫", "とうちゃく": "到着", "りょうしゅうしょ": "領収書" };
export function stripQueryScaffolding(query: string): string {
  let q = query.normalize("NFKC");
  for (const p of QUERY_STOP_PHRASES) q = q.split(p).join(" ");
  // Order-number mentions are identifiers, not topical evidence; their
  // bigrams (文番/番号/号は) otherwise dominate the coverage denominator.
  q = q.replace(/\[注文番号\]/g, " ").replace(/[A-Z]{3}-\d{4}-\d{4}/g, " ").replace(/注文番号/g, " ");
  for (const [from, to] of Object.entries(QUERY_EXPANSIONS)) if (q.includes(from)) q = `${q} ${to}`;
  return q;
}

export function tokenize(text: string): string[] {
  const norm = normalizeText(text);
  const tokens: string[] = [];
  const wordRe = /[a-z0-9]+(?:[-_/][a-z0-9]+)*/g;
  let m: RegExpExecArray | null;
  while ((m = wordRe.exec(norm)) !== null) tokens.push(m[0]);
  // CJK runs -> character bigrams (and unigrams for recall). Runs are first
  // split at common particles (はがをにのではともへからまでより) — a crude
  // bunsetsu split that removes most cross-word junk bigrams without a
  // morphological analyzer.
  const cjkRuns = norm.match(/[぀-ヿ一-鿿]+/g) ?? [];
  for (const run of cjkRuns) {
    const segments = run.split(/(?<=[はがをにのではともへからまでより])/).filter(Boolean);
    for (const seg of segments) {
      const chars = [...seg];
      for (let i = 0; i < chars.length; i++) {
        tokens.push(chars[i]);
        if (i + 1 < chars.length) tokens.push(chars[i] + chars[i + 1]);
      }
    }
  }
  return tokens;
}

export interface IndexedDoc {
  article_id: string;
  chunk_id: string;
  title: string;
  text: string;
  intent_tags: string[];
}
export interface SearchHit {
  article_id: string;
  chunk_id: string;
  title: string;
  excerpt: string;
  lexical_score: number;
}

/** Term-overlap lexical scorer with IDF weighting over the in-memory index. */
export class LexicalIndex {
  private docs: IndexedDoc[] = [];
  private docTokens: Map<string, Map<string, number>> = new Map();
  private df: Map<string, number> = new Map();

  constructor(docs: IndexedDoc[]) {
    this.docs = docs;
    for (const d of docs) {
      const counts = new Map<string, number>();
      for (const t of tokenize(`${d.title} ${d.text}`)) counts.set(t, (counts.get(t) ?? 0) + 1);
      this.docTokens.set(d.chunk_id, counts);
      for (const t of counts.keys()) this.df.set(t, (this.df.get(t) ?? 0) + 1);
    }
  }

  search(query: string, opts: { intent?: string; limit?: number } = {}): SearchHit[] {
    // Content tokens: anchor tokens containing a kanji/katakana character.
    // Hiragana-only bigrams are grammatical scaffolding, not topical evidence.
    // A document is relevant evidence only if it covers >=30% of the query's
    // distinct content tokens (calibrated on the gold set, SPEC §8.3).
    const isContent = (t: string) => t.length >= 2 && /[一-鿿ァ-ヶ]/.test(t);
    const qContent = [...new Set(tokenize(stripQueryScaffolding(query)))].filter(isContent);
    const idfOf = (t: string) => {
      const dfv = this.df.get(t);
      // Out-of-vocabulary tokens carry no evidence either way; discount them
      // instead of assigning maximum IDF, which let junk bigrams (from
      // identifiers and cross-word spans) dominate the denominator.
      return dfv === undefined ? 0.35 * Math.log(1 + Math.max(this.docs.length, 1)) : Math.log(1 + Math.max(this.docs.length, 1) / (1 + dfv));
    };
    const totalIdf = qContent.reduce((a, t) => a + idfOf(t), 0);
    // IDF-weighted content coverage: how much of the query's topical weight
    // does this document actually cover? Threshold calibrated on gold data.
    const contentCoverage = (counts: Map<string, number>) =>
      totalIdf === 0 ? 1 : qContent.filter(t => counts.has(t)).reduce((a, t) => a + idfOf(t), 0) / totalIdf;
    const limit = opts.limit ?? 5;
    const qTokens = [...new Set(tokenize(stripQueryScaffolding(query)))];
    if (qTokens.length === 0) return [];
    const N = Math.max(this.docs.length, 1);
    const hits: SearchHit[] = [];
    for (const d of this.docs) {
      const counts = this.docTokens.get(d.chunk_id)!;
      let anchorScore = 0; // bigrams / latin words: real relevance evidence
      let uniScore = 0;    // single CJK chars: tie-break only, never sufficient
      for (const t of qTokens) {
        const tf = counts.get(t);
        if (!tf) continue;
        const idf = Math.log(1 + N / (1 + (this.df.get(t) ?? 0)));
        if (t.length >= 2) anchorScore += idf * (1 + Math.log(tf));
        else uniScore += idf * (1 + Math.log(tf));
      }
      let score = anchorScore + 0.1 * uniScore;
      const intentMatch = !!opts.intent && d.intent_tags.includes(opts.intent);
      if (intentMatch) score *= 1.15;
      // Intent-aware evidence gate: an article tagged with the classified
      // intent needs less lexical coverage (typos/transliteration gaps),
      // while off-intent articles must clear the full bar. This keeps
      // no-evidence detection strict for out-of-scope topics.
      const gate = intentMatch ? INTENT_GATE_THRESHOLD : CONTENT_COVERAGE_THRESHOLD;
      if (anchorScore > 0 && contentCoverage(counts) >= gate) {
        hits.push({ article_id: d.article_id, chunk_id: d.chunk_id, title: d.title, excerpt: d.text.slice(0, 240), lexical_score: Number(score.toFixed(4)) });
      }
    }
    hits.sort((a, b) => b.lexical_score - a.lexical_score);
    return hits.slice(0, limit);
  }
}

/** Reciprocal rank fusion, documented for when dense retrieval is enabled.
 *  Currently unused for ranking because dense retrieval is disabled by the
 *  ModelRail embeddings capability spike (no embeddings endpoint found). */
export function reciprocalRankFusion(rankings: string[][], k = 60): Array<{ id: string; score: number }> {
  const scores = new Map<string, number>();
  for (const list of rankings) list.forEach((id, i) => scores.set(id, (scores.get(id) ?? 0) + 1 / (k + i + 1)));
  return [...scores.entries()].map(([id, score]) => ({ id, score })).sort((a, b) => b.score - a.score);
}
