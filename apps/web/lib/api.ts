export const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:4100";

export type User = { id: string; email: string; role: string; tenant_id: string };
export type Inquiry = { id: string; subject: string; redacted_body?: string; body?: string; status: string; state?: string; created_at: string; order_no_hint?: string | null; intent?: string; urgency?: string };
export type RunSummary = { id: string; state: string; inquiry_id?: string; trace_id?: string; failure_code?: string | null; created_at?: string };
export type RunStep = { step_key: string; state: string; attempt: number };
export type ToolCall = { id?: string; server?: string; tool?: string; tool_name?: string; status?: string; state?: string; latency_ms?: number; args_redacted?: unknown };
export type Draft = { id: string; revision: number; body: string; cited_article_ids: string[]; verification_json: unknown; content_hash: string; state: string };
export type ProposedAction = { id: string; action_type: string; state: string; payload_hash: string; canonical_payload?: unknown };
export type Escalation = { reason_code: string; summary: string };
export type Evidence = { article_id: string; title: string; excerpt: string };
export type RunDetail = { run: { id: string; state: string; trace_id?: string; failure_code?: string | null }; steps: RunStep[]; tool_calls: ToolCall[]; draft: Draft | null; proposed_actions: ProposedAction[]; escalation: Escalation | null; evidence: Evidence[] };
export type KbArticle = { id: string; title: string; body?: string; intent_tags?: string[] | string; status?: string; version?: number; updated_at?: string };
export type Ticket = { id: string; inquiry_id?: string; status?: string; priority?: string; created_at?: string; title?: string; subject?: string };
export type OutboxItem = { id: string; inquiry_id?: string; body?: string; delivery_state: string; created_at?: string; draft_revision?: number };
export type EvalRun = { id: string; created_at?: string; status?: string; state?: string; dataset_version?: string; summary?: unknown; metrics?: unknown; total?: number; passed?: number };
export type AuditEvent = { id: string; action: string; entity_type?: string; entity_id?: string; actor_id?: string; actor_type?: string; created_at?: string; trace_id?: string };

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem("benri_token");
}
export function getStoredUser(): User | null {
  if (typeof window === "undefined") return null;
  try { const raw = localStorage.getItem("benri_user"); return raw ? (JSON.parse(raw) as User) : null; } catch { return null; }
}
export function setSession(token: string, user: User) { localStorage.setItem("benri_token", token); localStorage.setItem("benri_user", JSON.stringify(user)); }
export function clearSession() { localStorage.removeItem("benri_token"); localStorage.removeItem("benri_user"); }

export async function apiFetch<T>(path: string, options: RequestInit = {}): Promise<T> {
  const token = getToken();
  const headers: Record<string, string> = { "Content-Type": "application/json", ...((options.headers as Record<string, string>) || {}) };
  if (token) headers["Authorization"] = `Bearer ${token}`;
  const res = await fetch(`${API_URL}${path}`, { ...options, headers });
  if (res.status === 401) {
    if (typeof window !== "undefined" && !window.location.pathname.startsWith("/login")) { window.location.href = "/login"; }
    throw new Error("Unauthorized");
  }
  if (!res.ok) {
    let msg = `Request failed: ${res.status}`;
    try { const j = (await res.json()) as { error?: string; message?: string; code?: string }; msg = j.message || j.error || j.code || msg; } catch { /* ignore */ }
    throw new Error(msg);
  }
  if (res.status === 204) return {} as T;
  return (await res.json()) as T;
}

// helpers to normalise list responses
export function extractList<T>(data: unknown, keys: string[]): T[] {
  if (Array.isArray(data)) return data as T[];
  if (data && typeof data === "object") {
    const obj = data as Record<string, unknown>;
    for (const k of keys) { if (Array.isArray(obj[k])) return obj[k] as T[]; }
  }
  return [];
}
export function formatDate(v?: string | null): string {
  if (!v) return "-";
  try { return new Date(v).toLocaleString("ja-JP"); } catch { return v; }
}
export function stringifyVerification(v: unknown): string[] {
  if (!v) return [];
  if (Array.isArray(v)) return v.map((x) => typeof x === "string" ? x : JSON.stringify(x));
  if (typeof v === "object") {
    const obj = v as Record<string, unknown>;
    // common shapes: {checks:[...]}, {passed, checks}, or plain map
    if (Array.isArray(obj.checks)) return (obj.checks as unknown[]).map((c) => {
      if (typeof c === "string") return c;
      if (c && typeof c === "object") { const cc = c as Record<string, unknown>; return `${cc.name || cc.key || "check"}: ${cc.passed === true || cc.status === "passed" ? "passed" : cc.passed === false || cc.status === "failed" ? "failed" : String(cc.status ?? cc.result ?? JSON.stringify(c))}${cc.message ? ` — ${String(cc.message)}` : ""}`; }
      return JSON.stringify(c);
    });
    if (typeof obj.passed === "boolean") return [`overall: ${obj.passed ? "passed" : "failed"}`, ...Object.entries(obj).filter(([k]) => k !== "checks" && k !== "passed").map(([k, val]) => `${k}: ${typeof val === "object" ? JSON.stringify(val) : String(val)}`)];
    return Object.entries(obj).map(([k, val]) => `${k}: ${typeof val === "object" ? JSON.stringify(val) : String(val)}`);
  }
  return [String(v)];
}
