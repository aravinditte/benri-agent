import type { components } from "./api/schema";

export const API_URL = process.env.NEXT_PUBLIC_API_BASE_URL || process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

// Types are derived from the canonical OpenAPI contract (api/openapi.yaml)
// via openapi-typescript — see lib/api/schema.d.ts (regenerate with
// `npm run generate:api`). Do not hand-maintain parallel DTOs here.
export type User = components["schemas"]["User"];
export type Inquiry = components["schemas"]["Inquiry"];
export type RunSummary = components["schemas"]["RunSummary"];
export type RunStep = components["schemas"]["RunStep"];
export type ToolCall = components["schemas"]["ToolCall"];
// The Go API returns `verification` as a parsed object and
// `cited_article_ids` as a string array. `verification_json` is kept as an
// optional legacy field only so older payload shapes (v2 returned it as a
// raw string in some paths) still type-check at the review page, which
// reads `draft.verification ?? draft.verification_json`.
export type Draft = components["schemas"]["Draft"] & { verification_json?: unknown };
export type ProposedAction = components["schemas"]["ProposedAction"];
export type Escalation = components["schemas"]["Escalation"];
export type Evidence = components["schemas"]["Evidence"];
export type RunDetail = components["schemas"]["RunDetail"];
export type KbArticle = components["schemas"]["Article"];
export type Ticket = components["schemas"]["Ticket"];
export type OutboxItem = components["schemas"]["OutboxMessage"];
export type EvalRun = components["schemas"]["EvalRunSummary"];
export type AuditEvent = components["schemas"]["AuditEvent"];

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
    try {
      // Go API error envelope: { error: { code, message } }; v2 used flat fields.
      const j = (await res.json()) as { error?: string | { code?: string; message?: string }; message?: string; code?: string };
      msg = j.message || (typeof j.error === "string" ? j.error : j.error?.message || j.error?.code) || j.code || msg;
    } catch { /* ignore */ }
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
      if (c && typeof c === "object") { const cc = c as Record<string, unknown>; return `${cc.name || cc.key || "check"}: ${cc.passed === true || cc.status === "passed" ? "passed" : cc.passed === false || cc.status === "failed" ? "failed" : String(cc.status ?? cc.result ?? JSON.stringify(c))}${cc.message ? ` — ${String(cc.message)}` : cc.detail ? ` — ${String(cc.detail)}` : ""}`; }
      return JSON.stringify(c);
    });
    if (typeof obj.passed === "boolean") return [`overall: ${obj.passed ? "passed" : "failed"}`, ...Object.entries(obj).filter(([k]) => k !== "checks" && k !== "passed").map(([k, val]) => `${k}: ${typeof val === "object" ? JSON.stringify(val) : String(val)}`)];
    return Object.entries(obj).map(([k, val]) => `${k}: ${typeof val === "object" ? JSON.stringify(val) : String(val)}`);
  }
  return [String(v)];
}
