"use client";
import React, { createContext, useCallback, useContext, useEffect, useState } from "react";

export type Lang = "ja" | "en";
const dict = {
  appName: { ja: "Benri Agent — 運用ワークベンチ", en: "Benri Agent — Operator Workbench" },
  queue: { ja: "キュー", en: "Queue" },
  escalations: { ja: "エスカレーション", en: "Escalations" },
  admin: { ja: "管理", en: "Admin" },
  login: { ja: "ログイン", en: "Login" },
  logout: { ja: "ログアウト", en: "Logout" },
  email: { ja: "メールアドレス", en: "Email" },
  password: { ja: "パスワード", en: "Password" },
  signIn: { ja: "ログインする", en: "Sign in" },
  signingIn: { ja: "ログイン中...", en: "Signing in..." },
  demoAccounts: { ja: "デモアカウント: operator@hinata.example / demo-operator-pass、admin@hinata.example / demo-admin-pass", en: "Demo: operator@hinata.example / demo-operator-pass, admin@hinata.example / demo-admin-pass" },
  state: { ja: "状態", en: "State" },
  status: { ja: "ステータス", en: "Status" },
  subject: { ja: "件名", en: "Subject" },
  created: { ja: "作成日時", en: "Created" },
  createdAt: { ja: "作成日時", en: "Created at" },
  runAgent: { ja: "エージェント実行", en: "Run agent" },
  running: { ja: "実行中...", en: "Running..." },
  loading: { ja: "読み込み中...", en: "Loading..." },
  noInquiries: { ja: "問い合わせはありません", en: "No inquiries" },
  refresh: { ja: "更新", en: "Refresh" },
  inquiryDetail: { ja: "問い合わせ詳細", en: "Inquiry detail" },
  redactedBody: { ja: "問い合わせ本文（秘匿化済み）", en: "Inquiry body (redacted)" },
  orderHint: { ja: "注文番号ヒント", en: "Order no. hint" },
  runs: { ja: "実行履歴", en: "Runs" },
  runDetail: { ja: "実行詳細", en: "Run detail" },
  steps: { ja: "ステップ", en: "Steps" },
  step: { ja: "ステップ", en: "Step" },
  attempt: { ja: "試行", en: "Attempt" },
  toolCalls: { ja: "ツール呼び出し", en: "Tool calls" },
  evidence: { ja: "根拠記事", en: "Evidence" },
  draft: { ja: "返信ドラフト", en: "Draft" },
  draftBody: { ja: "ドラフト本文", en: "Draft body" },
  saveRevision: { ja: "新しいリビジョンとして保存", en: "Save as new revision" },
  saving: { ja: "保存中...", en: "Saving..." },
  verification: { ja: "検証結果", en: "Verification results" },
  proposedActions: { ja: "提案アクション", en: "Proposed actions" },
  approveSelected: { ja: "選択したアクションを承認", en: "Approve selected" },
  reject: { ja: "却下", en: "Reject" },
  rejectReason: { ja: "却下理由", en: "Reject reason" },
  escalation: { ja: "エスカレーション", en: "Escalation" },
  reasonCode: { ja: "理由コード", en: "Reason code" },
  handoffSummary: { ja: "引き継ぎサマリー", en: "Handoff summary" },
  traceId: { ja: "トレースID", en: "Trace ID" },
  failureCode: { ja: "失敗コード", en: "Failure code" },
  revision: { ja: "リビジョン", en: "Revision" },
  contentHash: { ja: "コンテンツハッシュ", en: "Content hash" },
  citedArticles: { ja: "引用記事", en: "Cited articles" },
  kbArticles: { ja: "ナレッジベース記事", en: "KB Articles" },
  evalRuns: { ja: "評価実行", en: "Eval runs" },
  audit: { ja: "監査ログ", en: "Audit" },
  tickets: { ja: "チケット", en: "Tickets" },
  outbox: { ja: "送信箱（デモ）", en: "Outbox (demo)" },
  deliveryState: { ja: "配信状態", en: "Delivery state" },
  title: { ja: "タイトル", en: "Title" },
  body: { ja: "本文", en: "Body" },
  tags: { ja: "タグ（カンマ区切り）", en: "Tags (comma separated)" },
  createArticle: { ja: "記事を作成", en: "Create article" },
  save: { ja: "保存", en: "Save" },
  edit: { ja: "編集", en: "Edit" },
  cancel: { ja: "キャンセル", en: "Cancel" },
  action: { ja: "アクション", en: "Action" },
  entity: { ja: "エンティティ", en: "Entity" },
  actor: { ja: "実行者", en: "Actor" },
  backToQueue: { ja: "キューに戻る", en: "Back to queue" },
  noEscalations: { ja: "エスカレーションはありません", en: "No escalations" },
  noDraft: { ja: "ドラフトはまだありません。エージェントを実行してください。", en: "No draft yet. Run the agent." },
  noEvidence: { ja: "根拠記事はありません", en: "No evidence" },
  noVerification: { ja: "検証結果はありません", en: "No verification results" },
  noActions: { ja: "提案アクションはありません", en: "No proposed actions" },
  error: { ja: "エラー", en: "Error" },
  selectRun: { ja: "実行を選択", en: "Select a run" },
} as const;
export type DictKey = keyof typeof dict;

type Ctx = { lang: Lang; setLang: (l: Lang) => void; toggle: () => void; t: (k: DictKey) => string };
const LanguageContext = createContext<Ctx>({ lang: "ja", setLang: () => {}, toggle: () => {}, t: (k) => dict[k].ja });
export function LanguageProvider({ children }: { children: React.ReactNode }) {
  const [lang, setLangState] = useState<Lang>("ja");
  useEffect(() => { const s = localStorage.getItem("benri_lang"); if (s === "en" || s === "ja") setLangState(s); }, []);
  const setLang = useCallback((l: Lang) => { setLangState(l); localStorage.setItem("benri_lang", l); }, []);
  const toggle = useCallback(() => { setLang(lang === "ja" ? "en" : "ja"); }, [lang, setLang]);
  const t = useCallback((k: DictKey) => dict[k][lang], [lang]);
  return <LanguageContext.Provider value={{ lang, setLang, toggle, t }}>{children}</LanguageContext.Provider>;
}
export function useLang() { return useContext(LanguageContext); }
