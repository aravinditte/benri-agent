"use client";
import { FormEvent, useCallback, useEffect, useState } from "react";
import AuthGuard from "@/components/AuthGuard";
import { apiFetch, extractList, formatDate, getStoredUser, type AuditEvent, type EvalRun, type KbArticle, type OutboxItem, type Ticket } from "@/lib/api";
import { useLang } from "@/lib/i18n";

type Tab = "kb" | "evals" | "audit" | "tickets" | "outbox";

function Inner() {
  const { t } = useLang();
  const [tab, setTab] = useState<Tab>("kb");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [articles, setArticles] = useState<KbArticle[]>([]);
  const [evals, setEvals] = useState<EvalRun[]>([]);
  const [audit, setAudit] = useState<AuditEvent[]>([]);
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [outbox, setOutbox] = useState<OutboxItem[]>([]);
  const [loading, setLoading] = useState(true);
  const [isAdmin, setIsAdmin] = useState(false);
  // KB form
  const [newTitle, setNewTitle] = useState("");
  const [newBody, setNewBody] = useState("");
  const [newTags, setNewTags] = useState("");
  const [editing, setEditing] = useState<KbArticle | null>(null);
  const [editTitle, setEditTitle] = useState("");
  const [editBody, setEditBody] = useState("");
  const [editTags, setEditTags] = useState("");

  useEffect(() => { const u = getStoredUser(); setIsAdmin(u?.role === "admin"); }, []);

  const load = useCallback(async () => {
    setLoading(true);
    try { const d = await apiFetch<unknown>("/kb/articles"); setArticles(extractList<KbArticle>(d, ["articles", "items", "data"])); } catch (e) { setError(e instanceof Error ? e.message : "KB load failed"); }
    try { const d = await apiFetch<unknown>("/evals/runs"); setEvals(extractList<EvalRun>(d, ["runs", "eval_runs", "items", "data"])); } catch { setEvals([]); }
    try { const d = await apiFetch<unknown>("/audit"); setAudit(extractList<AuditEvent>(d, ["events", "audit", "items", "data"])); } catch { setAudit([]); }
    try { const d = await apiFetch<unknown>("/tickets"); setTickets(extractList<Ticket>(d, ["tickets", "items", "data"])); } catch { setTickets([]); }
    try { const d = await apiFetch<unknown>("/outbox"); setOutbox(extractList<OutboxItem>(d, ["outbox", "items", "messages", "data"])); } catch { setOutbox([]); }
    setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  async function createArticle(e: FormEvent) {
    e.preventDefault(); setError(""); setMessage("");
    try { await apiFetch("/kb/articles", { method: "POST", body: JSON.stringify({ title: newTitle, body: newBody, intent_tags: newTags.split(",").map((s) => s.trim()).filter(Boolean) }) }); setNewTitle(""); setNewBody(""); setNewTags(""); setMessage("Created"); await load(); } catch (err) { setError(err instanceof Error ? err.message : "Create failed"); }
  }
  function startEdit(a: KbArticle) { setEditing(a); setEditTitle(a.title); setEditBody(a.body || ""); setEditTags(Array.isArray(a.intent_tags) ? a.intent_tags.join(", ") : (a.intent_tags || "")); }
  async function saveEdit(e: FormEvent) {
    e.preventDefault(); if (!editing) return; setError("");
    try { await apiFetch(`/kb/articles/${editing.id}`, { method: "PATCH", body: JSON.stringify({ title: editTitle, body: editBody, intent_tags: editTags.split(",").map((s) => s.trim()).filter(Boolean) }) }); setEditing(null); setMessage("Saved"); await load(); } catch (err) { setError(err instanceof Error ? err.message : "Save failed"); }
  }

  return (
    <div className="container">
      <h1 style={{ fontSize: 18 }}>{t("admin")}</h1>
      {error ? <div className="error">{error}</div> : null}
      {message ? <div className="success">{message}</div> : null}
      <div className="tabs">
        {(["kb", "evals", "audit", "tickets", "outbox"] as Tab[]).map((k) => <button key={k} className={`tab ${tab === k ? "active" : ""}`} onClick={() => setTab(k)}>{k === "kb" ? t("kbArticles") : k === "evals" ? t("evalRuns") : k === "audit" ? t("audit") : k === "tickets" ? t("tickets") : t("outbox")}</button>)}
        <button className="btn secondary small" onClick={() => void load()}>{t("refresh")}</button>
      </div>
      {loading ? <div className="card"><p className="muted">{t("loading")}</p></div> : null}

      {tab === "kb" && !loading ? (
        <>
          <div className="card"><h3>{t("kbArticles")} ({articles.length})</h3>
            {articles.length === 0 ? <p className="muted">-</p> : <table><thead><tr><th>{t("title")}</th><th>{t("status")}</th><th>{t("tags")}</th><th></th></tr></thead><tbody>{articles.map((a) => <tr key={a.id}><td>{a.title}<div className="muted mono">{a.id} v{a.version ?? "-"}</div></td><td>{a.status || "-"}</td><td>{Array.isArray(a.intent_tags) ? a.intent_tags.join(", ") : a.intent_tags || "-"}</td><td>{isAdmin ? <button className="btn secondary small" onClick={() => startEdit(a)}>{t("edit")}</button> : null}</td></tr>)}</tbody></table>}
          </div>
          {editing ? (
            <div className="card"><h3>{t("edit")}: {editing.id}</h3><form onSubmit={saveEdit}><label>{t("title")}</label><input className="input" value={editTitle} onChange={(e) => setEditTitle(e.target.value)} required /><label>{t("body")}</label><textarea className="input" value={editBody} onChange={(e) => setEditBody(e.target.value)} rows={8} /><label>{t("tags")}</label><input className="input" value={editTags} onChange={(e) => setEditTags(e.target.value)} /><div className="flex" style={{ marginTop: 10 }}><button className="btn" type="submit">{t("save")}</button><button className="btn secondary" type="button" onClick={() => setEditing(null)}>{t("cancel")}</button></div></form></div>
          ) : null}
          {isAdmin ? (
            <div className="card"><h3>{t("createArticle")}</h3><form onSubmit={createArticle}><label>{t("title")}</label><input className="input" value={newTitle} onChange={(e) => setNewTitle(e.target.value)} required /><label>{t("body")}</label><textarea className="input" value={newBody} onChange={(e) => setNewBody(e.target.value)} rows={6} /><label>{t("tags")}</label><input className="input" value={newTags} onChange={(e) => setNewTags(e.target.value)} /><div style={{ marginTop: 10 }}><button className="btn" type="submit">{t("createArticle")}</button></div></form></div>
          ) : <p className="muted">KB editing requires admin role.</p>}
        </>
      ) : null}

      {tab === "evals" && !loading ? <div className="card"><h3>{t("evalRuns")} ({evals.length})</h3>{evals.length === 0 ? <p className="muted">-</p> : <table><thead><tr><th>ID</th><th>{t("status")}</th><th>{t("created")}</th><th>Summary</th></tr></thead><tbody>{evals.map((r) => <tr key={r.id}><td className="mono">{r.id}</td><td>{r.status || r.state || "-"}</td><td>{formatDate(r.created_at)}</td><td><pre style={{ margin: 0 }}>{JSON.stringify(r.summary ?? r.metrics ?? { total: r.total, passed: r.passed, dataset: r.dataset_version }, null, 2)}</pre></td></tr>)}</tbody></table>}</div> : null}

      {tab === "audit" && !loading ? <div className="card"><h3>{t("audit")} ({audit.length})</h3>{audit.length === 0 ? <p className="muted">-</p> : <table><thead><tr><th>{t("created")}</th><th>{t("action")}</th><th>{t("entity")}</th><th>{t("actor")}</th><th>{t("traceId")}</th></tr></thead><tbody>{audit.map((ev) => <tr key={ev.id}><td>{formatDate(ev.created_at)}</td><td>{ev.action}</td><td>{ev.entity_type || ""} <span className="mono muted">{ev.entity_id || ""}</span></td><td>{ev.actor_type || ""} {ev.actor_id || ""}</td><td className="mono">{ev.trace_id || "-"}</td></tr>)}</tbody></table>}</div> : null}

      {tab === "tickets" && !loading ? <div className="card"><h3>{t("tickets")} ({tickets.length})</h3>{tickets.length === 0 ? <p className="muted">-</p> : <table><thead><tr><th>ID</th><th>{t("status")}</th><th>{t("created")}</th></tr></thead><tbody>{tickets.map((tk) => <tr key={tk.id}><td className="mono">{tk.id}</td><td>{tk.status || "-"}</td><td>{formatDate(tk.created_at)}</td></tr>)}</tbody></table>}</div> : null}

      {tab === "outbox" && !loading ? <div className="card"><h3>{t("outbox")} ({outbox.length})</h3>{outbox.length === 0 ? <p className="muted">-</p> : outbox.map((o) => <div key={o.id} style={{ borderBottom: "1px solid #eef0f3", padding: "8px 0" }}><div className="flex"><span className="badge">{o.delivery_state}</span><span className="muted">{formatDate(o.created_at)}</span><span className="mono muted">{o.id}</span></div>{o.body ? <p style={{ whiteSpace: "pre-wrap" }}>{o.body}</p> : null}<p className="muted">Sent in demo outbox — not delivered</p></div>)}</div> : null}
    </div>
  );
}
export default function AdminPage() { return <AuthGuard><Inner /></AuthGuard>; }
