// CRM MCP tool implementation (SPEC §7): read-only lookup_order over
// synthetic fixture data. Returns a summary WITHOUT raw contact details.
import type { BenriStore } from "@benri/db";

export function lookupOrderTool(store: BenriStore, input: { tenant_id: string; order_no: string }) {
  const row = store.findOrderByNo(input.tenant_id, input.order_no);
  if (!row) return null;
  return {
    order_no: String(row.order_no), item: String(row.item), ordered_at: String(row.ordered_at),
    delivery_status: String(row.delivery_status), customer_name: String(row.customer_name),
  };
}
