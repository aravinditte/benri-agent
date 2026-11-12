// MCP v2 stdio interop test: spawns the real KB/CRM servers as subprocesses
// and calls tools through the official @modelcontextprotocol/client.
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { Client } from "@modelcontextprotocol/client";
import { StdioClientTransport } from "@modelcontextprotocol/client/stdio";
import { BenriStore, seedInquiries, seedStore } from "@benri/db";

const dir = mkdtempSync(join(tmpdir(), "benri-mcp-"));
const dbPath = join(dir, "mcp.sqlite");
let kb: Client; let crm: Client;

async function connect(serverRel: string): Promise<Client> {
  const transport = new StdioClientTransport({
    command: "npx", args: ["tsx", join(process.cwd(), serverRel)],
    env: { ...process.env, BENRI_DB_PATH: dbPath, BENRI_TENANT_ID: "tenant-hinata" } as Record<string, string>,
  });
  const client = new Client({ name: "benri-test", version: "1.0.0" });
  await client.connect(transport);
  return client;
}
beforeAll(async () => {
  const store = new BenriStore(dbPath, "full");
  seedStore(store);
  seedInquiries(store, (t: string) => t.normalize("NFKC"));
  store.close();
  kb = await connect("packages/mcp-kb/src/server.ts");
  crm = await connect("packages/mcp-crm/src/server.ts");
}, 60000);
afterAll(async () => { await kb?.close(); await crm?.close(); });

describe("MCP v2 stdio servers", () => {
  it("kb server lists search_kb and get_article", async () => {
    const tools = await kb.listTools();
    expect(tools.tools.map(t => t.name).sort()).toEqual(["get_article", "search_kb"]);
  });
  it("search_kb returns the delivery article for a delivery query", async () => {
    const res = await kb.callTool({ name: "search_kb", arguments: { query: "配送の日数を教えてください", intent: "delivery_status", limit: 5 } });
    const hits = JSON.parse((res.content as Array<{ text: string }>)[0].text) as Array<{ article_id: string }>;
    expect(hits.length).toBeGreaterThan(0);
    expect(hits.map(h => h.article_id)).toContain("KB-002");
  });
  it("lookup_order returns the fixture order; unknown order errors cleanly", async () => {
    const res = await crm.callTool({ name: "lookup_order", arguments: { order_no: "HIN-2026-0001" } });
    const order = JSON.parse((res.content as Array<{ text: string }>)[0].text) as { delivery_status: string };
    expect(order.delivery_status).toBeTruthy();
    const missing = await crm.callTool({ name: "lookup_order", arguments: { order_no: "HIN-9999-9999" } });
    expect(missing.isError).toBe(true);
  });
});
