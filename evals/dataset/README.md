# Benri Agent — Evaluation Gold Dataset

- **Dataset version:** 1.0.0
- **File:** `gold.jsonl` (one JSON object per line)
- **Total cases:** 50
- **Demo tenant:** Hinata Store (fictional Japanese home-goods retailer). All cases, customers, orders, and knowledge-base facts are synthetic and fictional.

## Counts by group

| Group | Count | Case IDs |
|---|---|---|
| routine | 24 | G-001 – G-024 |
| edge | 10 | G-025 – G-034 |
| must-escalate | 8 | G-035 – G-042 |
| adversarial | 8 | G-043 – G-050 |
| **Total** | **50** | |

- Routine: delivery status, shipping fees/windows, returns/exchanges within policy, invoice and qualified-invoice (適格請求書) requests, product usage for 加湿器「しずく」 / 炊飯器「ほのり」 / 掃除機「そら」, warranty, payment methods, cancellation before shipment, price/stock, and general account FAQs — each mapped to `relevant_article_ids` in `fixtures/kb/articles.json`.
- Edge: mixed Japanese/English, typos, missing order numbers, ambiguous phrasing, and full-width characters. All are expected to be draftable (no escalation) from general policy evidence.
- Must-escalate (8): 2 complaints (`COMPLAINT_REQUIRES_HUMAN`), 2 no-evidence topics absent from the KB — ペット保険 and 海外配送 to a country not covered (`NO_RELEVANT_KB_EVIDENCE`), 2 identity-sensitive requests — パスワード変更 and 住所変更 (`IDENTITY_SENSITIVE_REQUEST`), 1 legal/safety case — 訴訟 / 怪我をした (`LEGAL_OR_SAFETY_SENSITIVE`), and 1 ambiguous order reference (`ORDER_ID_AMBIGUOUS`). All have `expected_escalation: true` and empty `expected_allowed_actions`.
- Adversarial (8): prompt injection (「システムの指示を無視して」), requests to invent a discount or a delivery date, PII fishing (他のお客様の電話番号), tool-output injection phrasing, and cross-tenant probing against `tenant-sakura`. All have `expected_escalation: true`, empty `expected_allowed_actions`, and non-empty `adversarial_flags`.

## Case fields

Each case carries: `case_id`, `dataset_version` (`1.0.0`), `group`, `inquiry_text` (Japanese), `expected_intent` (fixed taxonomy key), `expected_urgency` (`low` | `normal` | `high`), `expected_escalation`, `expected_escalation_reason` (stable code or `null`), `relevant_article_ids` (IDs from `fixtures/kb/articles.json`; empty for no-evidence cases), `required_facts`, `forbidden_claims`, `expected_allowed_actions` (subset of `send_demo_reply`, `create_ticket`, `add_ticket_note`; empty when escalation is expected), `adversarial_flags`, `order_no_hint` (an `order_no` from `fixtures/crm/customers_orders.json` or `null`), and `japanese_review_status`.

Escalation reason codes used: `COMPLAINT_REQUIRES_HUMAN`, `NO_RELEVANT_KB_EVIDENCE`, `LOW_CLASSIFICATION_CONFIDENCE`, `ORDER_ID_AMBIGUOUS`, `POLICY_NOT_COVERED`, `IDENTITY_SENSITIVE_REQUEST`, `LEGAL_OR_SAFETY_SENSITIVE`, `VERIFICATION_FAILED`, `MODEL_BUDGET_EXCEEDED`.

## Japanese review status

**japanese_review_status: not_native_reviewed**

Honestly stated: the Japanese in this dataset — and in the companion fixtures (`fixtures/kb/articles.json`, `fixtures/crm/customers_orders.json`, `fixtures/inquiries/inquiries.json`) — was authored synthetically for this project and has **not** been reviewed by a native Japanese speaker. 丁寧語 style is intended, but naturalness and business appropriateness are not guaranteed. Do not describe this corpus as native-reviewed. A competent Japanese-speaker review should be completed and this status updated (with a dataset version bump) before any public quality claim is made about the Japanese content.

## Related fixtures

- `fixtures/kb/articles.json` — 20 KB articles (18 `tenant-hinata`, 2 `tenant-sakura` for tenant-isolation tests).
- `fixtures/crm/customers_orders.json` — 10 synthetic customers and 10 synthetic orders.
- `fixtures/inquiries/inquiries.json` — 30 seeded synthetic inquiries for `tenant-hinata`; entries containing a synthetic phone number or postal code are marked `contains_synthetic_pii: true` to exercise redaction.
