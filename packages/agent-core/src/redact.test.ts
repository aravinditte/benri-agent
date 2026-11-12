import { describe, expect, it } from "vitest";
import { redactPII } from "./redact.js";
describe("redaction", () => {
  it("redacts JP phone, postal, email after NFKC", () => {
    const r = redactPII("連絡先は０９０-１２３４-５６７８、〒100-0001、test@example.jp です");
    expect(r.text).not.toContain("090-1234-5678");
    expect(r.text).not.toContain("100-0001");
    expect(r.text).not.toContain("test@example.jp");
    expect(r.kinds).toContain("phone_hyphen");
  });
  it("masks fixture order numbers without eating them as postal codes", () => {
    const r = redactPII("注文番号はHIN-2026-0001です");
    expect(r.text).toBe("注文番号は[注文番号]です");
    expect(r.kinds).toContain("order_no");
  });
  it("leaves ordinary Japanese text untouched", () => {
    const r = redactPII("配送状況を教えてください。");
    expect(r.text).toBe("配送状況を教えてください。");
    expect(r.kinds).toEqual([]);
  });
});
