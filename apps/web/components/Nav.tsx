"use client";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState } from "react";
import { clearSession, getStoredUser, type User } from "@/lib/api";
import { useLang } from "@/lib/i18n";

export default function Nav() {
  const { lang, setLang, t } = useLang();
  const pathname = usePathname();
  const router = useRouter();
  const [user, setUser] = useState<User | null>(null);
  useEffect(() => { setUser(getStoredUser()); }, [pathname]);
  if (pathname.startsWith("/login")) return null;
  function logout() { clearSession(); router.push("/login"); }
  return (
    <header className="topnav">
      <Link href="/" className="brand">{t("appName")}</Link>
      <nav>
        <Link href="/">{t("queue")}</Link>
        <Link href="/escalations">{t("escalations")}</Link>
        <Link href="/admin">{t("admin")}</Link>
      </nav>
      <div className="spacer" />
      {user ? <span className="user">{user.email} ({user.role})</span> : null}
      <div className="langtoggle" aria-label="Language">
        <button type="button" className={lang === "ja" ? "active" : ""} onClick={() => setLang("ja")}>JP</button>
        <button type="button" className={lang === "en" ? "active" : ""} onClick={() => setLang("en")}>EN</button>
      </div>
      <button type="button" className="btn ghost small" onClick={logout}>{t("logout")}</button>
    </header>
  );
}
