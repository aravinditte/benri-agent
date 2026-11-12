// MCP ticket gateway: spawns the write-only ticket MCP server over stdio.
// Only the executor hosts this server (SPEC §7). A transport timeout after
// the request was sent is ambiguous -> OutcomeUnknownError.
import { Client } from "@modelcontextprotocol/client";
import { StdioClientTransport } from "@modelcontextprotocol/client/stdio";
import { join } from "node:path";
import { OutcomeUnknownError, type TicketGateway } from "./execute.js";

export class McpTicketGateway implements TicketGateway {
  private client?: Client;
  async init(dbPath: string) {
    const transport = new StdioClientTransport({
      command: "npx",
      args: ["tsx", join(process.cwd(), "packages/mcp-ticket/src/server.ts")],
      env: { ...process.env, BENRI_DB_PATH: dbPath } as Record<string, string>,
    });
    this.client = new Client({ name: "benri-executor", version: "1.0.0" });
    await this.client.connect(transport);
  }
  private async call(name: string, actionId: string): Promise<Record<string, unknown>> {
    let res;
    try {
      res = await this.client!.callTool({ name, arguments: { approved_action_id: actionId } });
    } catch {
      throw new OutcomeUnknownError(`ticket MCP call ${name} outcome unknown`);
    }
    const content = res.content as Array<{ type: string; text?: string }>;
    const parsed = JSON.parse(content.find(c => c.type === "text")?.text ?? "{}") as Record<string, unknown>;
    if (res.isError) {
      const err = new Error(String(parsed.error ?? "TICKET_TOOL_ERROR"));
      (err as { code?: string }).code = String(parsed.error ?? "TICKET_TOOL_ERROR");
      err.name = "TicketToolError";
      throw Object.assign(err, { code: String(parsed.error ?? "TICKET_TOOL_ERROR") });
    }
    return parsed;
  }
  async createTicket(id: string) { return (await this.call("create_ticket", id)) as { ticket_id: string; idempotent_replay: boolean }; }
  async addTicketNote(id: string) { return (await this.call("add_ticket_note", id)) as { ticket_id: string; note_id: string; idempotent_replay: boolean }; }
  async close() { await this.client?.close(); }
}
