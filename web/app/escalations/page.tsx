"use client";
import { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import AuthGuard from "@/components/AuthGuard";
import { apiFetch, extractList, formatDate, type Inquiry, type RunDetail, type RunSummary } from "@/lib/api";
import { useLang } from "@/lib/i18n";

type Row = { inquiry: Inquiry; reason_code: string; summary: string };

function Inner() {
  const { t } = useLang();
  const [rows, setRows] = useState<Row[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const data = await apiFetch<unknown>("/inquiries");
      const all = extractList<Inquiry>(data, ["inquiries", "items", "data"]);
      const escalated = all.filter((i) => i.status === "escalated" || i.status === "needs_review");
      // Try to enrich with escalation details; if inquiry itself is not escalated, still attempt for escalated-state filter above
      const strict = all.filter((i) => i.status === "escalated");
      const target = strict.length > 0 ? strict : escalated;
      const enriched: Row[] = [];
      for (const inq of target) {
        let reason = ""; let summary = "";
        try {
          const det = await apiFetch<{ inquiry: Inquiry; runs: RunSummary[] }>(`/inquiries/${inq.id}`);
          if (det.runs && det.runs.length > 0) {
            const rd = await apiFetch<RunDetail>(`/runs/${det.runs[0].id}`);
            if (rd.escalation) { reason = rd.escalation.reason_code; summary = rd.escalation.summary; }
            else if (rd.run.state === "escalated") { reason = rd.run.failure_code || "ESCALATED"; }
          }
        } catch { /* keep blank */ }
        enriched.push({ inquiry: inq, reason_code: reason, summary });
      }
      // If no inquiry has escalated status, also scan first runs for escalation objects (fallback for differing status names)
      if (enriched.length === 0) {
        for (const inq of all.slice(0, 30)) {
          try {
            const det = await apiFetch<{ inquiry: Inquiry; runs: RunSummary[] }>(`/inquiries/${inq.id}`);
            if (!det.runs?.length) continue;
            const rd = await apiFetch<RunDetail>(`/runs/${det.runs[0].id}`);
            if (rd.escalation) enriched.push({ inquiry: inq, reason_code: rd.escalation.reason_code, summary: rd.escalation.summary });
          } catch { /* ignore */ }
        }
      }
      setRows(enriched);
    } catch (e) { setError(e instanceof Error ? e.message : "Failed"); } finally { setLoading(false); }
  }, []);
  useEffect(() => { void load(); }, [load]);
  return (
    <div className="container">
      <div className="flex" style={{ justifyContent: "space-between" }}><h1 style={{ fontSize: 18 }}>{t("escalations")}</h1><button className="btn secondary" onClick={() => void load()}>{t("refresh")}</button></div>
      {error ? <div className="error">{error}</div> : null}
      <div className="card">
        {loading ? <p className="muted">{t("loading")}</p> : rows.length === 0 ? <p className="muted">{t("noEscalations")}</p> : (
          <table><thead><tr><th>{t("reasonCode")}</th><th>{t("subject")}</th><th>{t("handoffSummary")}</th><th>{t("created")}</th></tr></thead>
            <tbody>{rows.map((r) => <tr key={r.inquiry.id}><td>{r.reason_code ? <span className="badge warn">{r.reason_code}</span> : "-"}</td><td><Link href={`/inquiries/${r.inquiry.id}`}>{r.inquiry.subject}</Link><div className="muted mono">{r.inquiry.id}</div></td><td>{r.summary || "-"}</td><td>{formatDate(r.inquiry.created_at)}</td></tr>)}</tbody></table>
        )}
      </div>
    </div>
  );
}
export default function EscalationsPage() { return <AuthGuard><Inner /></AuthGuard>; }
