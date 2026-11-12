import { McpServer } from "@modelcontextprotocol/server";
import { StdioServerTransport } from "@modelcontextprotocol/server/stdio";
import * as z from "zod/v4";
import { BenriStore } from "@benri/db";
import { lookupOrderTool } from "./tools.js";

const store = new BenriStore(process.env.BENRI_DB_PATH ?? "./data/benri.sqlite", "agent");
const tenantId = process.env.BENRI_TENANT_ID ?? "tenant-hinata";
const server = new McpServer({ name: "benri-crm", version: "1.0.0" });
server.registerTool("lookup_order", {
  description: "Look up a synthetic fixture order by order number (read-only)",
  inputSchema: z.object({ order_no: z.string() }),
}, async ({ order_no }) => {
  const order = lookupOrderTool(store, { tenant_id: tenantId, order_no });
  return { content: [{ type: "text" as const, text: JSON.stringify(order) }], isError: order === null };
});
await server.connect(new StdioServerTransport());
