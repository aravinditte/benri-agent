// MCP tool gateway: spawns the read-only KB/CRM MCP servers over stdio and
// calls their tools. The agent worker hosts ONLY these read-only servers —
// it has no route to the ticket MCP server or the executor (SPEC §3.3, §7).
import { Client } from "@modelcontextprotocol/client";
import { StdioClientTransport } from "@modelcontextprotocol/client/stdio";
import { join } from "node:path";
import type { ToolGateway } from "@benri/agent-core";
import type { SearchHit } from "@benri/retrieval";

async function connect(serverRelPath: string, env: Record<string, string>): Promise<Client> {
  const transport = new StdioClientTransport({
    command: "npx",
    args: ["tsx", join(process.cwd(), serverRelPath)],
    env: { ...process.env, ...env } as Record<string, string>,
  });
  const client = new Client({ name: "benri-agent-worker", version: "1.0.0" });
  await client.connect(transport);
  return client;
}

export class McpToolGateway implements ToolGateway {
  private kb?: Client;
  private crm?: Client;
  constructor(private dbPath: string, private tenantId: string) {}
  async init() {
    const env = { BENRI_DB_PATH: this.dbPath, BENRI_TENANT_ID: this.tenantId };
    this.kb = await connect("packages/mcp-kb/src/server.ts", env);
    this.crm = await connect("packages/mcp-crm/src/server.ts", env);
  }
  private async call(client: Client, name: string, args: Record<string, unknown>): Promise<unknown> {
    const res = await client.callTool({ name, arguments: args });
    const content = res.content as Array<{ type: string; text?: string }>;
    const text = content.find(c => c.type === "text")?.text ?? "null";
    return JSON.parse(text);
  }
  async searchKb(_tenantId: string, query: string, intent: string, limit: number): Promise<SearchHit[]> {
    return (await this.call(this.kb!, "search_kb", { query, intent, limit })) as SearchHit[];
  }
  async lookupOrder(_tenantId: string, orderNo: string): Promise<Record<string, unknown> | null> {
    return (await this.call(this.crm!, "lookup_order", { order_no: orderNo })) as Record<string, unknown> | null;
  }
  async close() { await this.kb?.close(); await this.crm?.close(); }
}
