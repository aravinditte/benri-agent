// Deterministic Japanese mechanical style checks (SPEC §8.6).
// These check FORMAT and obvious register violations only. They do NOT prove
// natural business Japanese — that requires human review (not yet done:
// fixture corpus is marked not_native_reviewed).
export const REQUIRED_SIGNATURE = "ひなたストア カスタマーサポート";
export const BANNED_PHRASES = [
  "だよ", "だね", "じゃん", "っす", "マジ", "ヤバい",
  "絶対に補償", "必ず返金します", "100%保証", "無料にします", "割引します",
  "キャンペーン価格", "今だけ",
];
export const CASUAL_ENDINGS = [/だ[。!！]/, /だよね/, /なのだ[。]/];

export interface StyleResult { ok: boolean; violations: string[] }

export function checkJapaneseStyle(body: string): StyleResult {
  const violations: string[] = [];
  if (!body.includes(REQUIRED_SIGNATURE)) violations.push("MISSING_SIGNATURE");
  if (!/です|ます|ございます|いたします/.test(body)) violations.push("NO_POLITE_REGISTER_MARKERS");
  for (const p of BANNED_PHRASES) if (body.includes(p)) violations.push(`BANNED_PHRASE:${p}`);
  for (const re of CASUAL_ENDINGS) if (re.test(body)) violations.push("CASUAL_ENDING");
  if (body.length < 40) violations.push("TOO_SHORT");
  if (body.length > 2000) violations.push("TOO_LONG");
  return { ok: violations.length === 0, violations };
}
