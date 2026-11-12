// PII redaction (SPEC §5.1 step 0, §6.4). NFKC first, then pattern detectors.
// This is a LIMITED detector for common Japanese patterns — not proof of
// complete PII removal. Only the redacted body is ever persisted.
const PATTERNS: Array<{ re: RegExp; replacement: string; kind: string }> = [
  { re: /[A-Z]{3}-\d{4}-\d{4}/g, replacement: "[注文番号]", kind: "order_no" },
  { re: /[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}/g, replacement: "[メールアドレス]", kind: "email" },
  { re: /0\d{1,4}-\d{1,4}-\d{4}/g, replacement: "[電話番号]", kind: "phone_hyphen" },
  { re: /0\d{9,10}/g, replacement: "[電話番号]", kind: "phone_plain" },
  { re: /〒\s?\d{3}-\d{4}/g, replacement: "[郵便番号]", kind: "postal_marked" },
  { re: /(?<![\d-])\d{3}-\d{4}(?![\d-])/g, replacement: "[郵便番号]", kind: "postal" },
  { re: /(?<!\d)\d{4}\s?\d{4}\s?\d{4}(?!\d)/g, replacement: "[番号]", kind: "my_number_like" },
];
export interface RedactionResult { text: string; kinds: string[] }
export function redactPII(input: string): RedactionResult {
  let text = input.normalize("NFKC");
  const kinds: string[] = [];
  for (const p of PATTERNS) {
    if (p.re.test(text)) kinds.push(p.kind);
    text = text.replace(p.re, p.replacement);
  }
  return { text, kinds };
}
export function maskOrderNo(orderNo: string): string {
  return orderNo.length <= 4 ? "****" : `****${orderNo.slice(-4)}`;
}
