"use client";
import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { getToken } from "@/lib/api";
import { useLang } from "@/lib/i18n";

export default function AuthGuard({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { t } = useLang();
  const [ready, setReady] = useState(false);
  useEffect(() => {
    if (!getToken()) { router.replace("/login"); } else { setReady(true); }
  }, [router]);
  if (!ready) return <div className="container"><p className="muted">{t("loading")}</p></div>;
  return <>{children}</>;
}
