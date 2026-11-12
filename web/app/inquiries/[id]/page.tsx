"use client";
import { useCallback, useEffect, useState } from "react";
import { useParams } from "next/navigation";
import Link from "next/link";
import AuthGuard from "@/components/AuthGuard";
import { apiFetch, formatDate, stringifyVerification, type Draft, type Inquiry, type RunDetail, type RunSummary } from "@/lib/api";
import { useLang } from "@/lib/i18n";

function DetailInner() {
  const { t } = useLang();
  const params = useParams<{ id: string }>();
  const id = params.id;
  const [inquiry, setInquiry] = useState<Inquiry | null>(null);
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [selectedRunId, setSelectedRunId] = useState<string>("");
  const [detail, setDetail] = useState<RunDetail | null>(null);
  const [draftBody, setDraftBody] = useState("");
  const [draft, setDraft] = useState<Draft | null>(null);
  const [selectedActions, setSelectedActions] = useState<Record<string, boolean>>({});
  const [rejectReason, setRejectReason] = useState("");
  const [error, setError] = useState("");
  const [message, setMessage] = useState("");
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);

  const loadRun = useCallback(async (runId: string) => {
    if (!runId) { setDetail(null); setDraft(null); return; }
    const d = await apiFetch<RunDetail>(`/runs/${runId}`);
    setDetail(d);
    if (d.draft) { setDraft(d.draft); setDraftBody(d.draft.body); } else { setDraft(null); setDraftBody(""); }
    const sel: Record<string, boolean> = {};
    (d.proposed_actions || []).forEach((a) => { sel[a.id] = false; });
    setSelectedActions(sel);
  }, []);

  const load = useCallback(async () => {
    setLoading(true); setError("");
    try {
      const data = await apiFetch<{ inquiry: Inquiry; runs: RunSummary[] }>(`/inquiries/${id}`);
      setInquiry(data.inquiry);
      const r = data.runs || [];
      setRuns(r);
      if (r.length > 0) { setSelectedRunId(r[0].id); await loadRun(r[0].id); }
    } catch (e) { setError(e instanceof Error ? e.message : "Failed to load"); } finally { setLoading(false); }
  }, [id, loadRun]);
  useEffect(() => { void load(); }, [load]);

  async function onSelectRun(runId: string) { setSelectedRunId(runId); setError(""); setMessage(""); try { await loadRun(runId); } catch (e) { setError(e instanceof Error ? e.message : "Failed"); } }
  async function runAgent() { setBusy(true); setError(""); try { await apiFetch(`/inquiries/${id}/runs`, { method: "POST", body: JSON.stringify({}) }); await load(); } catch (e) { setError(e instanceof Error ? e.message : "Failed"); } finally { setBusy(false); } }

  async function saveRevision() {
    if (!draft) return; setBusy(true); setError(""); setMessage("");
    try {
      const nd = await apiFetch<Draft | { draft: Draft }>(`/drafts/${draft.id}/revisions`, { method: "POST", body: JSON.stringify({ body: draftBody }) });
      const next = ("draft" in (nd as object) ? (nd as { draft: Draft }).draft : nd) as Draft;
      // API may return revision object with different id; refetch if possible
      if (next && next.id) { try { const fresh = await apiFetch<Draft | { draft: Draft }>(`/drafts/${next.id}`); const fd = ("draft" in (fresh as object) ? (fresh as { draft: Draft }).draft : fresh) as Draft; setDraft(fd); setDraftBody(fd.body); } catch { setDraft(next); setDraftBody(next.body || draftBody); } }
      setMessage("Saved new revision");
      if (selectedRunId) { try { await loadRun(selectedRunId); } catch { /* keep edited draft */ } }
    } catch (e) { setError(e instanceof Error ? e.message : "Failed to save"); } finally { setBusy(false); }
  }

  async function approve() {
    if (!draft) return; setBusy(true); setError(""); setMessage("");
    const selected = Object.entries(selectedActions).filter(([, v]) => v).map(([k]) => k);
    try { const r = await apiFetch<{ approval_id: string; execution_job_ids: string[] }>(`/drafts/${draft.id}/approve`, { method: "POST", body: JSON.stringify({ revision: draft.revision, selected_action_ids: selected }) }); setMessage(`Approved: ${r.approval_id} / jobs: ${(r.execution_job_ids || []).join(", ") || "-"}`); } catch (e) { setError(e instanceof Error ? e.message : "Approve failed"); } finally { setBusy(false); }
  }
  async function reject() {
    if (!draft) return; if (!rejectReason.trim()) { setError("Reject reason is required"); return; } setBusy(true); setError("");
    try { await apiFetch(`/drafts/${draft.id}/reject`, { method: "POST", body: JSON.stringify({ reason: rejectReason }) }); setMessage("Rejected"); } catch (e) { setError(e instanceof Error ? e.message : "Reject failed"); } finally { setBusy(false); }
  }

  if (loading) return <div className="container"><p className="muted">{t("loading")}</p></div>;
  const activeDraft = draft ?? detail?.draft ?? null;
  const verificationLines = activeDraft ? stringifyVerification(activeDraft.verification ?? (activeDraft as Draft).verification_json) : [];

  return (
    <div className="container">
      <Link href="/">{t("backToQueue")}</Link>
      <h1 style={{ fontSize: 18 }}>{t("inquiryDetail")}</h1>
      {error ? <div className="error">{error}</div> : null}
      {message ? <div className="success">{message}</div> : null}
      {inquiry ? (
        <div className="card">
          <div className="flex"><span className="badge">{inquiry.status}</span><span className="muted">{formatDate(inquiry.created_at)}</span><span className="mono muted">{inquiry.id}</span></div>
          <h2 style={{ marginTop: 8 }}>{inquiry.subject}</h2>
          {inquiry.order_no_hint ? <p className="muted">{t("orderHint")}: {inquiry.order_no_hint}</p> : null}
          <h3>{t("redactedBody")}</h3>
          <pre>{inquiry.redacted_body || "-"}</pre>
          <div className="flex" style={{ marginTop: 10 }}>
            <button className="btn" disabled={busy} onClick={() => void runAgent()}>{t("runAgent")}</button>
            {runs.length > 0 ? (<><label style={{ margin: 0 }}>{t("selectRun")}</label><select className="input" style={{ width: 320 }} value={selectedRunId} onChange={(e) => void onSelectRun(e.target.value)}>{runs.map((r) => <option key={r.id} value={r.id}>{r.id} — {r.state}</option>)}</select></>) : null}
          </div>
        </div>
      ) : null}

      {detail ? (
        <>
          <div className="card">
            <h3>{t("runDetail")}</h3>
            <p className="mono">{detail.run.id} — {detail.run.state}</p>
            {detail.run.trace_id ? <p className="muted">{t("traceId")}: <span className="mono">{detail.run.trace_id}</span></p> : null}
            {detail.run.failure_code ? <p>{t("failureCode")}: <span className="badge danger">{detail.run.failure_code}</span></p> : null}
            <h3>{t("steps")}</h3>
            {detail.steps.length === 0 ? <p className="muted">-</p> : <ul className="timeline">{detail.steps.map((s, i) => <li key={`${s.step_key}-${s.attempt}-${i}`}><span className="badge">{s.state}</span><span>{s.step_key}</span><span className="muted">{t("attempt")} {s.attempt}</span></li>)}</ul>}
            <h3 style={{ marginTop: 12 }}>{t("toolCalls")}</h3>
            {!detail.tool_calls || detail.tool_calls.length === 0 ? <p className="muted">-</p> : <pre>{JSON.stringify(detail.tool_calls, null, 2)}</pre>}
          </div>

          {detail.escalation ? (
            <div className="warnbox" style={{ marginBottom: 14 }}>
              <h3 style={{ marginTop: 0 }}>{t("escalation")}</h3>
              <p>{t("reasonCode")}: <span className="badge warn">{detail.escalation.reason_code}</span></p>
              <p>{t("handoffSummary")}: {detail.escalation.summary}</p>
            </div>
          ) : null}

          <div className="grid2">
            <div className="card">
              <h3>{t("evidence")}</h3>
              {!detail.evidence || detail.evidence.length === 0 ? <p className="muted">{t("noEvidence")}</p> : detail.evidence.map((ev) => (<div key={ev.article_id} style={{ borderBottom: "1px solid #eef0f3", padding: "8px 0" }}><div className="flex"><strong>{ev.title}</strong><span className="mono muted">{ev.article_id}</span></div><p style={{ margin: "4px 0" }}>{ev.excerpt}</p></div>))}
            </div>
            <div className="card">
              <h3>{t("verification")}</h3>
              {verificationLines.length === 0 ? <p className="muted">{t("noVerification")}</p> : <ul>{verificationLines.map((line, i) => <li key={i}>{line}</li>)}</ul>}
              {draft ? <><p className="muted">{t("citedArticles")}: {(draft.cited_article_ids || []).join(", ") || "-"}</p><p className="muted">{t("contentHash")}: <span className="mono">{draft.content_hash}</span></p></> : null}
            </div>
          </div>

          <div className="card">
            <h3>{t("draft")}</h3>
            {!draft ? <p className="muted">{t("noDraft")}</p> : (
              <>
                <p className="muted">{t("revision")} {draft.revision} — {draft.state} — <span className="mono">{draft.id}</span></p>
                <label htmlFor="draftBody">{t("draftBody")}</label>
                <textarea id="draftBody" className="input" value={draftBody} onChange={(e) => setDraftBody(e.target.value)} rows={10} />
                <div className="flex" style={{ marginTop: 8 }}><button className="btn secondary" disabled={busy || draftBody === draft.body} onClick={() => void saveRevision()}>{t("saveRevision")}</button></div>
                <h3 style={{ marginTop: 16 }}>{t("proposedActions")}</h3>
                {!detail.proposed_actions || detail.proposed_actions.length === 0 ? <p className="muted">{t("noActions")}</p> : detail.proposed_actions.map((a) => (
                  <label key={a.id} style={{ display: "flex", gap: 8, alignItems: "center", fontWeight: 400, fontSize: 14 }}>
                    <input type="checkbox" checked={!!selectedActions[a.id]} onChange={(e) => setSelectedActions((prev) => ({ ...prev, [a.id]: e.target.checked }))} />
                    <span>{a.action_type}</span><span className="badge">{a.state}</span><span className="mono muted">{a.payload_hash}</span>
                  </label>))}
                <div className="flex" style={{ marginTop: 14 }}>
                  <button className="btn" disabled={busy} onClick={() => void approve()}>{t("approveSelected")}</button>
                </div>
                <label htmlFor="rejectReason">{t("rejectReason")}</label>
                <input id="rejectReason" className="input" value={rejectReason} onChange={(e) => setRejectReason(e.target.value)} placeholder="Reason" />
                <div style={{ marginTop: 8 }}><button className="btn danger" disabled={busy} onClick={() => void reject()}>{t("reject")}</button></div>
              </>
            )}
          </div>
        </>
      ) : inquiry ? <div className="card"><p className="muted">{t("noDraft")}</p></div> : null}
    </div>
  );
}
export default function InquiryDetailPage() { return <AuthGuard><DetailInner /></AuthGuard>; }
