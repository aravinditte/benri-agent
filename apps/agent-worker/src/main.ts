// Agent worker: claims runs with a lease, executes the pipeline with an
// "agent"-role store handle (structurally read-only w.r.t. fixture writes).
import { BenriStore } from "@benri/db";
import { DirectToolGateway, limitsFromEnv, mockLlmRespond, runPipeline, type ToolGateway } from "@benri/agent-core";
import { ModelRailClient } from "@benri/modelrail-client";
import { Tracer } from "@benri/observability";
import { McpToolGateway } from "./mcp-gateway.js";

const dbPath = process.env.BENRI_DB_PATH ?? "./data/benri.sqlite";
const workerId = `agent-${process.pid}`;
const once = process.argv.includes("--once");

export function buildLlm(): ModelRailClient {
  const mock = process.env.MOCK_LLM === "1" || !process.env.MODELRAIL_BASE_URL;
  if (mock) return new ModelRailClient({}, mockLlmRespond);
  return new ModelRailClient({
    baseUrl: process.env.MODELRAIL_BASE_URL, apiKey: process.env.MODELRAIL_API_KEY,
    model: process.env.MODELRAIL_CHAT_MODEL, timeoutMs: 15000,
  }, mockLlmRespond);
}

export async function processOne(store: BenriStore, tools: ToolGateway): Promise<boolean> {
  const run = store.claimNextRun(workerId, 60_000);
  if (!run) return false;
  const llm = buildLlm();
  const tracer = new Tracer(undefined, process.env.KAIRO_OTLP_ENDPOINT);
  try {
    const result = await runPipeline({ store, runId: String(run.id), tools, llm, limits: limitsFromEnv(), tracer });
    console.log(JSON.stringify({ run_id: result.run_id, outcome: result.outcome, escalation_reason: result.escalation_reason, failure_code: result.failure_code, mock_llm: llm.mock }));
  } catch (e) {
    // CrashSimulatedError or unexpected: leave the run leased; it becomes
    // claimable again after lease expiry and resumes from checkpoints.
    console.error(JSON.stringify({ run_id: String(run.id), worker_error: (e as Error).name }));
  }
  return true;
}

async function main() {
  const store = new BenriStore(dbPath, "agent");
  let tools: ToolGateway;
  if (process.env.BENRI_USE_MCP === "1") {
    const gw = new McpToolGateway(dbPath, "tenant-hinata");
    await gw.init();
    tools = gw;
  } else {
    tools = new DirectToolGateway(store);
  }
  if (once) {
    await processOne(store, tools);
    if (tools instanceof McpToolGateway) await tools.close();
    store.close();
    return;
  }
  console.log(`agent-worker ${workerId} polling ${dbPath}`);
  for (;;) {
    const did = await processOne(store, tools);
    if (!did) await new Promise(r => setTimeout(r, 500));
  }
}
if (import.meta.url === `file://${process.argv[1]}`) void main();
