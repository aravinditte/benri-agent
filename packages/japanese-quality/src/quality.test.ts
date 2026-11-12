import { describe, expect, it } from "vitest";
import { checkJapaneseStyle, REQUIRED_SIGNATURE } from "./index.js";
describe("japanese style checks", () => {
  it("passes a polite signed draft", () => {
    const body = `お問い合わせありがとうございます。配送状況についてご案内いたします。通常2〜3営業日で発送いたします。\n${REQUIRED_SIGNATURE}`;
    expect(checkJapaneseStyle(body).ok).toBe(true);
  });
  it("fails missing signature and banned discount promise", () => {
    const r = checkJapaneseStyle("ご連絡ありがとうございます。特別に割引しますのでご安心くださいませ。詳細は追ってご連絡いたします。");
    expect(r.ok).toBe(false);
    expect(r.violations).toContain("MISSING_SIGNATURE");
    expect(r.violations.some(v => v.startsWith("BANNED_PHRASE"))).toBe(true);
  });
});
