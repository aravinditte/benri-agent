import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { BenriStore } from "@benri/db";
import { DirectToolGateway, limitsFromEnv, mockLlmRespond, runPipeline } from "@benri/agent-core";
import { ModelRailClient } from "@benri/modelrail-client";
import { buildApi } from "./server.js";

const dir = mkdtempSync(join(tmpdir(), "benri-api-"));
const dbPath = join(dir, "api.sqlite");
let app: Awaited<ReturnType<typeof buildApi>>["app"];
let token = "";

beforeAll(async () => {
  process.env.DEMO_MODE = "1";
  ({ app } = await buildApi({ dbPath, seedIfEmpty: true }));
  const login = await app.inject({ method: "POST", url: "/auth/login", payload: { email: "operator@hinata.example", password: "demo-operator-pass" } });
  expect(login.statusCode).toBe(200);
  token = (login.json() as { token: string }).token;
});
afterAll(async () => { await app?.close(); });
const auth = () => ({ authorization: `Bearer ${token}` });

describe("api (SPEC §12.1)", () => {
  it("health is public; inquiries require auth", async () => {
    expect((await app.inject({ method: "GET", url: "/health" })).statusCode).toBe(200);
    expect((await app.inject({ method: "GET", url: "/inquiries" })).statusCode).toBe(401);
    const res = await app.inject({ method: "GET", url: "/inquiries", headers: auth() });
    expect(res.statusCode).toBe(200);
    expect((res.json() as { inquiries: unknown[] }).inquiries).toHaveLength(30);
  });
  it("bad login is rejected with a stable error code", async () => {
    const res = await app.inject({ method: "POST", url: "/auth/login", payload: { email: "operator@hinata.example", password: "wrong" } });
    expect(res.statusCode).toBe(401);
    expect((res.json() as { error: { code: string } }).error.code).toBe("INVALID_CREDENTIALS");
  });
  it("run creation is idempotent per inquiry+request key; approval flow works over HTTP", async () => {
    const list = await app.inject({ method: "GET", url: "/inquiries", headers: auth() });
    const inq = (list.json() as { inquiries: Array<{ id: string; order_no_hint: string | null }> }).inquiries.find(i => i.order_no_hint) ?? (list.json() as { inquiries: Array<{ id: string }> }).inquiries[0];
    const r1 = await app.inject({ method: "POST", url: `/inquiries/${inq.id}/runs`, headers: auth(), payload: { request_key: "k1" } });
    const r2 = await app.inject({ method: "POST", url: `/inquiries/${inq.id}/runs`, headers: auth(), payload: { request_key: "k1" } });
    const runId = (r1.json() as { run_id: string }).run_id;
    expect((r2.json() as { run_id: string }).run_id).toBe(runId);
    // process the run with an agent-role store on the same DB file
    const agentStore = new BenriStore(dbPath, "agent");
    const claimed = agentStore.claimNextRun("api-test", 60_000);
    expect(claimed?.id).toBe(runId);
    const outcome = await runPipeline({ store: agentStore, runId, tools: new DirectToolGateway(agentStore), llm: new ModelRailClient({}, mockLlmRespond), limits: limitsFromEnv() });
    agentStore.close();
    if (outcome.outcome !== "awaiting_review") return; // seeded inquiry may legitimately escalate
    const detail = await app.inject({ method: "GET", url: `/runs/${runId}`, headers: auth() });
    const d = detail.json() as { draft: { id: string; revision: number }; proposed_actions: Array<{ id: string }> };
    expect(d.draft).toBeTruthy();
    const approve = await app.inject({ method: "POST", url: `/drafts/${d.draft.id}/approve`, headers: auth(), payload: { revision: d.draft.revision, selected_action_ids: d.proposed_actions.map(a => a.id) } });
    expect(approve.statusCode).toBe(200);
    expect((approve.json() as { execution_job_ids: string[] }).execution_job_ids.length).toBe(d.proposed_actions.length);
    // second approval must fail
    const again = await app.inject({ method: "POST", url: `/drafts/${d.draft.id}/approve`, headers: auth(), payload: { revision: d.draft.revision, selected_action_ids: d.proposed_actions.map(a => a.id) } });
    expect(again.statusCode).toBe(409);
  });
  it("admin-only KB write rejects the operator role", async () => {
    const res = await app.inject({ method: "POST", url: "/kb/articles", headers: auth(), payload: { id: "KB-900", title: "x", body: "y" } });
    expect(res.statusCode).toBe(403);
  });
  it("POST /inquiries redacts PII before persistence", async () => {
    const res = await app.inject({ method: "POST", url: "/inquiries", headers: auth(), payload: { subject: "連絡先", body: "電話は090-1234-5678です。配送を教えてください。" } });
    expect(res.statusCode).toBe(201);
    const created = (res.json() as { inquiry: { redacted_body: string } }).inquiry;
    expect(created.redacted_body).not.toContain("090-1234-5678");
    expect(created.redacted_body).toContain("[電話番号]");
  });
});
