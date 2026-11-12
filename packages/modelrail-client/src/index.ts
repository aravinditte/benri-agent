// ModelRail gateway client (SPEC §10.1) + deterministic mock LLM.
// Embeddings capability spike result (2026-11-12): the sibling ModelRail
// codebase (~/workspace/modelrail) contains NO embeddings endpoint
// (grep for "embedding" over its Python sources returns nothing; only
// chat/completions is implemented). Dense retrieval is therefore DISABLED
// and Benri ships lexical-only retrieval. Never silently bypassed.
export interface ChatMessage { role: "system" | "user" | "assistant"; content: string }
export interface ChatResult {
  content: string;
  usage: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
  request_id?: string; model: string; latency_ms: number; mock: boolean;
}
export interface TransportResponse { status: number; json: () => Promise<unknown>; }
export type Transport = (url: string, init: { method: string; headers: Record<string, string>; body: string; signal: AbortSignal }) => Promise<TransportResponse>;

export class GatewayError extends Error {
  code = "GATEWAY_UNAVAILABLE";
  constructor(message: string) { super(message); this.name = "GatewayError"; }
}

export interface ModelRailConfig {
  baseUrl?: string; apiKey?: string; model?: string;
  timeoutMs?: number; maxResponseBytes?: number;
  transport?: Transport;
}

const defaultTransport: Transport = async (url, init) => {
  const res = await fetch(url, init);
  return { status: res.status, json: () => res.json() };
};

export class ModelRailClient {
  readonly mock: boolean;
  private transport: Transport;
  constructor(private cfg: ModelRailConfig = {}, mockLlm?: (msgs: ChatMessage[]) => string) {
    this.mock = !cfg.baseUrl;
    this.transport = cfg.transport ?? defaultTransport;
    this.mockFn = mockLlm;
  }
  private mockFn?: (msgs: ChatMessage[]) => string;

  async chat(messages: ChatMessage[], opts: { maxTokens?: number } = {}): Promise<ChatResult> {
    const started = Date.now();
    if (this.mock) {
      if (!this.mockFn) throw new GatewayError("MOCK_LLM enabled but no mock responder configured");
      const content = this.mockFn(messages);
      const promptTokens = Math.ceil(messages.reduce((a, m) => a + m.content.length, 0) / 2);
      const completionTokens = Math.ceil(content.length / 2);
      return { content, usage: { prompt_tokens: promptTokens, completion_tokens: completionTokens, total_tokens: promptTokens + completionTokens }, model: "mock-deterministic-v1", latency_ms: Date.now() - started, mock: true };
    }
    const timeoutMs = this.cfg.timeoutMs ?? 15000;
    let res: TransportResponse;
    try {
      res = await this.transport(`${this.cfg.baseUrl!.replace(/\/$/, "")}/v1/chat/completions`, {
        method: "POST",
        headers: { "content-type": "application/json", ...(this.cfg.apiKey ? { authorization: `Bearer ${this.cfg.apiKey}` } : {}) },
        body: JSON.stringify({ model: this.cfg.model ?? "default", messages, max_tokens: opts.maxTokens ?? 1200 }),
        signal: AbortSignal.timeout(timeoutMs),
      });
    } catch (e) {
      throw new GatewayError(`ModelRail request failed: ${(e as Error).name}`);
    }
    if (res.status < 200 || res.status >= 300) throw new GatewayError(`ModelRail HTTP ${res.status}`);
    const data = (await res.json()) as { id?: string; model?: string; choices?: Array<{ message?: { content?: string } }>; usage?: { prompt_tokens: number; completion_tokens: number; total_tokens: number } };
    const content = data.choices?.[0]?.message?.content;
    if (typeof content !== "string") throw new GatewayError("ModelRail response missing choices[0].message.content");
    return { content, usage: data.usage ?? { prompt_tokens: 0, completion_tokens: 0, total_tokens: 0 }, request_id: data.id, model: data.model ?? this.cfg.model ?? "unknown", latency_ms: Date.now() - started, mock: false };
  }

  /** Embeddings are NOT available: spike found no endpoint in ModelRail. */
  async embeddings(_input: string[]): Promise<never> {
    throw new GatewayError("EMBEDDINGS_UNSUPPORTED: ModelRail exposes no embeddings endpoint (capability spike 2026-11-12); dense retrieval disabled");
  }
}
