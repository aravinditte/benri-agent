import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import type { BenriStore } from "./sqlite-store.js";
import { hashPassword, contentHash } from "./password.js";

const root = join(dirname(fileURLToPath(import.meta.url)), "..", "..", "..");
const readJson = (p: string) => JSON.parse(readFileSync(join(root, p), "utf8"));

export const DEMO_USERS = [
  { id: "user-operator", tenant_id: "tenant-hinata", email: "operator@hinata.example", password: "demo-operator-pass", role: "operator" },
  { id: "user-admin", tenant_id: "tenant-hinata", email: "admin@hinata.example", password: "demo-admin-pass", role: "admin" },
];

export function seedStore(store: BenriStore) {
  store.insertTenant("tenant-hinata", "Hinata Store");
  store.insertTenant("tenant-sakura", "Sakura Living (synthetic isolation tenant)");
  for (const u of DEMO_USERS) {
    store.insertUser({ id: u.id, tenant_id: u.tenant_id, email: u.email, password_hash: hashPassword(u.password), role: u.role });
  }
  const articles = readJson("fixtures/kb/articles.json") as Array<Record<string, unknown>>;
  for (const a of articles) {
    store.insertArticle({
      id: String(a.article_id), tenant_id: String(a.tenant_id), title: String(a.title),
      intent_tags: (a.intent_tags as string[]) ?? [], body: String(a.body),
      version: Number(a.version ?? 1), status: String(a.status ?? "active"),
    });
  }
  const crm = readJson("fixtures/crm/customers_orders.json") as { customers: Array<Record<string, unknown>>; orders: Array<Record<string, unknown>> };
  for (const c of crm.customers) {
    store.insertCustomer({ id: String(c.customer_id), tenant_id: String(c.tenant_id), synthetic_name: String(c.synthetic_name), synthetic_contact_ref: String(c.synthetic_contact_ref) });
  }
  for (const o of crm.orders) {
    store.insertOrder({ id: `ord-${String(o.order_no)}`, tenant_id: String(o.tenant_id), customer_id: String(o.customer_id), order_no: String(o.order_no), item: String(o.item), ordered_at: String(o.ordered_at), delivery_status: String(o.delivery_status) });
  }
  // Inquiries are seeded with redaction applied by the caller-provided fn to
  // avoid a db->agent-core dependency cycle; seed-cli passes the real redactor.
  return { articles: articles.length, customers: crm.customers.length, orders: crm.orders.length };
}

export function seedInquiries(store: BenriStore, redact: (text: string) => string) {
  const inquiries = readJson("fixtures/inquiries/inquiries.json") as Array<Record<string, unknown>>;
  for (const q of inquiries) {
    const redacted = redact(String(q.body));
    store.insertInquiry({
      id: String(q.inquiry_id), tenant_id: "tenant-hinata", subject: String(q.subject),
      redacted_body: redacted, normalized_body_hash: contentHash(redacted),
      order_no_hint: (q.order_no_hint as string | null) ?? null,
    });
  }
  return inquiries.length;
}
