"use client";
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import AuthGuard from "@/components/AuthGuard";
import { apiFetch, extractList, formatDate, type Inquiry, type OutboxItem, type Ticket } from "@/lib/api";
import { useLang } from "@/lib/i18n";

function QueueInner() {
  const { t } = useLang();
  const router = useRouter();
  const [inquiries, setInquiries] = useState<Inquiry[]>([]);
  const [tickets, setTickets] = useState<Ticket[]>([]);
  const [outbox, setOutbox] = useState<OutboxItem[]>([]);
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(true);
  const [runningId, setRunningId] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const data = await apiFetch<unknown>("/inquiries");
      setInquiries(extractList<Inquiry>(data, ["inquiries", "items", "data"]));
    } catch (e) { setError(e instanceof Error ? e.message : "Failed to load"); }
    // tickets/outbox are secondary; don't block queue on them
    try { const d = await apiFetch<unknown>("/tickets"); setTickets(extractList<Ticket>(d, ["tickets", "items", "data"])); } catch { setTickets([]); }
    try { const d = await apiFetch<unknown>("/outbox"); setOutbox(extractList<OutboxItem>(d, ["outbox", "items", "messages", "data"])); } catch { setOutbox([]); }
    setLoading(false);
  }, []);
  useEffect(() => { void load(); }, [load]);

  async function runAgent(id: string, ev: React.MouseEvent) {
    ev.stopPropagation(); setRunningId(id); setError("");
    try {
      const idem = `web-${id}-${Date.now()}`;
      await apiFetch<{ run_id: string }>(`/inquiries/${id}/runs`, { method: "POST", headers: { "Idempotency-Key": idem }, body: JSON.stringify({}) });
      router.push(`/inquiries/${id}`);
    } catch (e) { setError(e instanceof Error ? e.message : "Failed to run"); } finally { setRunningId(null); }
  }

  return (
    <div className="container">
      <div className="flex" style={{ justifyContent: "space-between" }}><h1 style={{ fontSize: 18, margin: "4px 0" }}>{t("queue")}</h1><button className="btn secondary" onClick={() => void load()}>{t("refresh")}</button></div>
      {error ? <div className="error">{error}</div> : null}
      <div className="card">
        {loading ? <p className="muted">{t("loading")}</p> : inquiries.length === 0 ? <p className="muted">{t("noInquiries")}</p> : (
          <table><thead><tr><th>{t("state")}</th><th>{t("subject")}</th><th>{t("created")}</th><th></th></tr></thead>
            <tbody>{inquiries.map((inq) => (
              <tr key={inq.id} className="clickable" onClick={() => router.push(`/inquiries/${inq.id}`)}>
                <td><span className={`badge ${inq.status === "escalated" ? "danger" : inq.status === "failed" ? "danger" : ""}`}>{inq.status || inq.state || "-"}</span></td>
                <td>{inq.subject}<div className="muted mono">{inq.id}</div>{inq.order_no_hint ? <div className="muted">{t("orderHint")}: {inq.order_no_hint}</div> : null}</td>
                <td>{formatDate(inq.created_at)}</td>
                <td><button className="btn small" disabled={runningId === inq.id} onClick={(e) => void runAgent(inq.id, e)}>{runningId === inq.id ? t("running") : t("runAgent")}</button></td>
              </tr>))}
            </tbody></table>
        )}
      </div>

      <div className="grid2">
        <div className="card"><h3>{t("tickets")} ({tickets.length})</h3>
          {tickets.length === 0 ? <p className="muted">-</p> : <table><thead><tr><th>ID</th><th>{t("status")}</th><th>{t("created")}</th></tr></thead><tbody>{tickets.slice(0, 20).map((tk) => <tr key={tk.id}><td className="mono">{tk.id}</td><td>{tk.status || "-"}</td><td>{formatDate(tk.created_at)}</td></tr>)}</tbody></table>}
        </div>
        <div className="card"><h3>{t("outbox")} ({outbox.length})</h3>
          {outbox.length === 0 ? <p className="muted">-</p> : outbox.slice(0, 20).map((o) => (
            <div key={o.id} style={{ borderBottom: "1px solid #eef0f3", padding: "8px 0" }}>
              <div className="flex"><span className="badge">{o.delivery_state}</span><span className="muted">{formatDate(o.created_at)}</span></div>
              {o.body ? <p style={{ margin: "6px 0", whiteSpace: "pre-wrap" }}>{o.body}</p> : null}
              <p className="muted">Sent in demo outbox — not delivered</p>
            </div>))}
        </div>
      </div>
    </div>
  );
}
export default function QueuePage() { return <AuthGuard><QueueInner /></AuthGuard>; }
