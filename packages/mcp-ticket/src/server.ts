// Ticket MCP server over stdio (SPEC §7). Launched ONLY by the executor
// worker, with an executor-role store handle. Never exposed to the agent.
import { McpServer } from "@modelcontextprotocol/server";
import { StdioServerTransport } from "@modelcontextprotocol/server/stdio";
import * as z from "zod/v4";
import { BenriStore } from "@benri/db";
import { createTicketTool, addTicketNoteTool, TicketToolError } from "./tools.js";

const store = new BenriStore(process.env.BENRI_DB_PATH ?? "./data/benri.sqlite", "executor");
const server = new McpServer({ name: "benri-ticket", version: "1.0.0" });
const wrap = (fn: () => unknown) => {
  try { return { content: [{ type: "text" as const, text: JSON.stringify(fn()) }] }; }
  catch (e) { return { content: [{ type: "text" as const, text: JSON.stringify({ error: e instanceof TicketToolError ? e.code : "INTERNAL_ERROR" }) }], isError: true }; }
};
server.registerTool("create_ticket", {
  description: "Create a fixture ticket for an approved action (executor only). Takes approved_action_id only; payload is loaded server-side.",
  inputSchema: z.object({ approved_action_id: z.string() }),
}, async ({ approved_action_id }) => wrap(() => createTicketTool(store, { approved_action_id })));
server.registerTool("add_ticket_note", {
  description: "Add a note to the fixture ticket for an approved action (executor only).",
  inputSchema: z.object({ approved_action_id: z.string() }),
}, async ({ approved_action_id }) => wrap(() => addTicketNoteTool(store, { approved_action_id })));
await server.connect(new StdioServerTransport());
