import { BenriStore } from "@benri/db";
import { DirectTicketGateway, executeJob, type TicketGateway } from "./execute.js";
import { McpTicketGateway } from "./mcp-gateway.js";

const dbPath = process.env.BENRI_DB_PATH ?? "./data/benri.sqlite";
const workerId = `executor-${process.pid}`;
const once = process.argv.includes("--once");

export async function processOne(store: BenriStore, gateway: TicketGateway): Promise<boolean> {
  const job = store.claimNextJob(workerId, 60_000);
  if (!job) return false;
  if (job.state === "unknown") {
    console.log(JSON.stringify({ job_id: String(job.id), state: "unknown", note: "expired running job marked unknown; operator reconciliation required" }));
    return true;
  }
  const result = await executeJob(store, String(job.id), gateway);
  console.log(JSON.stringify(result));
  return true;
}

async function main() {
  const store = new BenriStore(dbPath, "executor");
  let gateway: TicketGateway;
  if (process.env.BENRI_USE_MCP === "1") {
    const gw = new McpTicketGateway();
    await gw.init(dbPath);
    gateway = gw;
  } else {
    gateway = new DirectTicketGateway(store);
  }
  if (once) {
    await processOne(store, gateway);
    if (gateway instanceof McpTicketGateway) await gateway.close();
    store.close();
    return;
  }
  console.log(`executor-worker ${workerId} polling ${dbPath}`);
  for (;;) {
    const did = await processOne(store, gateway);
    if (!did) await new Promise(r => setTimeout(r, 500));
  }
}
if (import.meta.url === `file://${process.argv[1]}`) void main();
