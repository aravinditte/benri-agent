// Deterministic keyword classifier used by the MOCK LLM responder.
// In live mode the model returns the same JSON shape; this classifier is the
// mock's implementation and is labelled as such. Confidence here is a routing
// signal only, not a calibrated probability (SPEC §5.4).
import type { Intent } from "./taxonomy.js";

const KEYWORDS: Record<Exclude<Intent, "complaint" | "other">, string[]> = {
  delivery_status: ["配送", "配達", "届き", "届く", "発送", "追跡", "荷物", "配送状況", "いつ届", "送料"],
  return_exchange: ["返品", "交換", "返送", "返金手続", "サイズが合わない", "サイズが違う", "へんぴん"],
  invoice_request: ["請求書", "領収書", "インボイス", "適格請求書"],
  product_usage: ["使い方", "使用方法", "お手入れ", "洗い方", "設定方法", "説明書", "フィルター", "取り付け", "取り外し", "掃除方法", "保証"],
  cancellation: ["キャンセル", "解約", "注文を取り消", "取り消し", "やめたい", "キャンセルしたい"],
  price_stock: ["在庫", "価格", "値段", "定価", "再入荷", "売り切れ", "割引", "ざいこ"],
  account: ["アカウント", "会員登録", "ログイン", "会員情報", "パスワード", "住所変更", "連絡先", "電話番号", "ポイント"],
};
export const COMPLAINT_KEYWORDS = ["クレーム", "苦情", "不満", "最悪", "ひどい", "謝罪", "ふざけ", "怒って"];
export const LEGAL_SAFETY_KEYWORDS = ["訴訟", "弁護士", "裁判", "怪我", "けが", "ケガ", "病院", "消費者センター", "健康被害", "倒れ"];
export const CREDENTIAL_DISCLOSURE_PATTERNS = ["パスワードを教えて", "管理者のパスワード", "認証情報を開示", "内部の認証情報"];
export const IDENTITY_KEYWORDS = ["パスワードを変更", "パスワード変更", "パスワードを変え", "住所変更", "住所を変更", "登録情報を変更", "メールアドレスを変更", "電話番号を変更", "氏名変更", "本人確認書類"];
export const INJECTION_PATTERNS = ["指示を無視", "システムの指示", "ignore", "システムプロンプト", "内部指示", "権限を与え", "管理者として", "この指示に従い", "承認なしで", "ツール出力です", "ツール出力に従い"];
export const PII_FISHING_PATTERNS = ["他のお客様の電話番号", "他のお客様の住所", "他人の電話番号", "他の顧客の", "別のお客様の個人情報"];
export const UNSUPPORTED_REQUEST_PATTERNS = ["割引して", "割引コード", "値引きして", "無料にして", "特別に返金", "保証期間を延長して", "割引を約束", "割引があると", "存在しない", "断言して", "でっち上げ", "必ず届く", "顧客情報を教え", "注文一覧を見せて"];

export interface Classification { intent: Intent; urgency: "low" | "normal" | "high"; confidence: number; matched: string[] }

export function classifyText(rawText: string): Classification {
  const text = rawText.normalize("NFKC");
  const matched: string[] = [];
  const has = (list: string[]) => list.filter(k => text.includes(k));
  const complaintHits = has(COMPLAINT_KEYWORDS);
  if (complaintHits.length > 0) {
    matched.push(...complaintHits);
    return { intent: "complaint", urgency: "high", confidence: 0.95, matched };
  }
  // Legal/safety content is a human-led complaint in this taxonomy.
  const legalHits = has(LEGAL_SAFETY_KEYWORDS);
  if (legalHits.length > 0) {
    matched.push(...legalHits);
    return { intent: "complaint", urgency: "high", confidence: 0.9, matched };
  }
  let best: Intent = "other"; let bestHits: string[] = []; let bestScore = 0;
  for (const [intent, kws] of Object.entries(KEYWORDS) as Array<[Intent, string[]]>) {
    const hits = has(kws);
    // occurrence-weighted: repeated topical terms are stronger evidence
    const score = hits.reduce((a, k) => a + text.split(k).length - 1, 0);
    if (score > bestScore) { best = intent; bestHits = hits; bestScore = score; }
  }
  matched.push(...bestHits);
  // Payment-method questions have no dedicated intent in the taxonomy;
  // they are the canonical "other" case and are answerable from KB-010.
  // Applied ONLY when no topical intent scored at all.
  if (bestScore === 0 && /支払い|お支払い|代金引換|コンビニ払い|銀行振込/.test(text)) {
    return { intent: "other", urgency: "normal", confidence: 0.8, matched: ["支払い"] };
  }
  const urgency = /至急|急ぎ|早急|困っています|困って/.test(text) ? "high" : /暇なとき|急ぎません/.test(text) ? "low" : "normal";
  const confidence = bestScore >= 2 ? 0.92 : bestScore === 1 ? 0.78 : 0.3;
  return { intent: best, urgency, confidence, matched };
}
