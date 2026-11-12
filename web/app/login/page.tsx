"use client";
import { FormEvent, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { API_URL, getToken, setSession, type User } from "@/lib/api";
import { useLang } from "@/lib/i18n";

export default function LoginPage() {
  const { t, lang, setLang } = useLang();
  const router = useRouter();
  const [email, setEmail] = useState("operator@hinata.example");
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const [loading, setLoading] = useState(false);

  useEffect(() => { if (getToken()) router.replace("/"); }, [router]);

  async function onSubmit(e: FormEvent) {
    e.preventDefault(); setError(""); setLoading(true);
    try {
      const res = await fetch(`${API_URL}/auth/login`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ email, password }) });
      if (!res.ok) throw new Error(`Login failed: ${res.status}`);
      const data = (await res.json()) as { token: string; user: User };
      setSession(data.token, data.user);
      router.push("/");
    } catch (err) { setError(err instanceof Error ? err.message : "Login failed"); } finally { setLoading(false); }
  }

  return (
    <div className="container loginwrap">
      <div className="flex" style={{ justifyContent: "space-between" }}>
        <h1 style={{ fontSize: 20, margin: 0 }}>{t("appName")}</h1>
        <div className="langtoggle" style={{ borderColor: "#d9dde3" }}>
          <button type="button" className={lang === "ja" ? "active" : ""} onClick={() => setLang("ja")} style={{ color: lang === "ja" ? "#111827" : "#344054" }}>JP</button>
          <button type="button" className={lang === "en" ? "active" : ""} onClick={() => setLang("en")} style={{ color: lang === "en" ? "#111827" : "#344054" }}>EN</button>
        </div>
      </div>
      <div className="card" style={{ marginTop: 16 }}>
        <h2>{t("login")}</h2>
        {error ? <div className="error">{error}</div> : null}
        <form onSubmit={onSubmit}>
          <label htmlFor="email">{t("email")}</label>
          <input id="email" className="input" type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoComplete="email" />
          <label htmlFor="password">{t("password")}</label>
          <input id="password" className="input" type="password" value={password} onChange={(e) => setPassword(e.target.value)} required autoComplete="current-password" />
          <div style={{ marginTop: 14 }}><button className="btn" type="submit" disabled={loading} style={{ width: "100%" }}>{loading ? t("signingIn") : t("signIn")}</button></div>
        </form>
        <p className="muted" style={{ marginTop: 12 }}>{t("demoAccounts")}</p>
      </div>
    </div>
  );
}
