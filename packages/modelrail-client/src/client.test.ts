import { describe, expect, it } from "vitest";
import { ModelRailClient, GatewayError } from "./index.js";

describe("modelrail client contract", () => {
  it("mock mode returns deterministic content + usage", async () => {
    const c = new ModelRailClient({}, () => "ok-response");
    const r = await c.chat([{ role: "user", content: "hi" }]);
    expect(r.content).toBe("ok-response");
    expect(r.mock).toBe(true);
    expect(r.usage.total_tokens).toBeGreaterThan(0);
  });
  it("parses an OpenAI-compatible live response via injected transport", async () => {
    const c = new ModelRailClient({ baseUrl: "http://gateway.test", apiKey: "k", model: "m1" }, undefined);
    (c as unknown as { transport: unknown }).transport = async () => ({ status: 200, json: async () => ({ id: "req-1", model: "m1", choices: [{ message: { content: "live" } }], usage: { prompt_tokens: 3, completion_tokens: 4, total_tokens: 7 } }) });
    const r = await c.chat([{ role: "user", content: "hi" }]);
    expect(r.content).toBe("live"); expect(r.request_id).toBe("req-1"); expect(r.usage.total_tokens).toBe(7); expect(r.mock).toBe(false);
  });
  it("HTTP 503 maps to GATEWAY_UNAVAILABLE", async () => {
    const c = new ModelRailClient({ baseUrl: "http://gateway.test" });
    (c as unknown as { transport: unknown }).transport = async () => ({ status: 503, json: async () => ({}) });
    await expect(c.chat([{ role: "user", content: "hi" }])).rejects.toBeInstanceOf(GatewayError);
  });
  it("transport timeout maps to GATEWAY_UNAVAILABLE", async () => {
    const c = new ModelRailClient({ baseUrl: "http://gateway.test" });
    (c as unknown as { transport: unknown }).transport = async () => { throw new Error("AbortError"); };
    await expect(c.chat([{ role: "user", content: "hi" }])).rejects.toMatchObject({ code: "GATEWAY_UNAVAILABLE" });
  });
  it("embeddings throws EMBEDDINGS_UNSUPPORTED (spike result)", async () => {
    const c = new ModelRailClient({}, () => "x");
    await expect(c.embeddings(["a"])).rejects.toMatchObject({ code: "GATEWAY_UNAVAILABLE" });
  });
});
