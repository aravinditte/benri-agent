// KB MCP server over stdio (SPEC §7). Launched by the agent worker only.
// Tenant is bound at launch via BENRI_TENANT_ID — never taken from tool args.
import { McpServer } from "@modelcontextprotocol/server";
import { StdioServerTransport } from "@modelcontextprotocol/server/stdio";
import * as z from "zod/v4";
import { BenriStore } from "@benri/db";
import { searchKbTool, getArticleTool } from "./tools.js";

const store = new BenriStore(process.env.BENRI_DB_PATH ?? "./data/benri.sqlite", "agent");
const tenantId = process.env.BENRI_TENANT_ID ?? "tenant-hinata";
const server = new McpServer({ name: "benri-kb", version: "1.0.0" });
server.registerTool("search_kb", {
  description: "Search the tenant knowledge base (read-only, lexical retrieval)",
  inputSchema: z.object({ query: z.string(), intent: z.string().optional(), limit: z.number().optional() }),
}, async ({ query, intent, limit }) => {
  const hits = searchKbTool(store, { tenant_id: tenantId, query, intent, limit });
  return { content: [{ type: "text" as const, text: JSON.stringify(hits) }] };
});
server.registerTool("get_article", {
  description: "Get a full KB article by id (read-only)",
  inputSchema: z.object({ article_id: z.string() }),
}, async ({ article_id }) => {
  const article = getArticleTool(store, { tenant_id: tenantId, article_id });
  return { content: [{ type: "text" as const, text: JSON.stringify(article) }], isError: article === null };
});
await server.connect(new StdioServerTransport());
