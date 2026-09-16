import { useEffect, useState } from "react";
import type { SupabaseClient, Session } from "@supabase/supabase-js";
import { loadConfig } from "./config";
import { createSupabase } from "./auth";
import { Login } from "./components/Login";
import { Dashboard } from "./components/Dashboard";

type Boot =
  | { phase: "loading" }
  | { phase: "error"; message: string }
  | { phase: "ready"; supabase: SupabaseClient };

// App boots the runtime config, builds the Supabase client, then tracks the auth
// session: no session shows the login screen, a session shows the dashboard. The
// binary serves this SPA regardless of go-api, so the shell always loads.
export function App() {
  const [boot, setBoot] = useState<Boot>({ phase: "loading" });
  const [session, setSession] = useState<Session | null>(null);

  useEffect(() => {
    let active = true;
    (async () => {
      try {
        const cfg = await loadConfig();
        const supabase = createSupabase(cfg);
        const { data } = await supabase.auth.getSession();
        if (!active) return;
        setSession(data.session);
        supabase.auth.onAuthStateChange((_event, s) => setSession(s));
        setBoot({ phase: "ready", supabase });
      } catch (err) {
        if (active) setBoot({ phase: "error", message: String(err) });
      }
    })();
    return () => {
      active = false;
    };
  }, []);

  if (boot.phase === "loading") {
    return <div className="login-screen"><p className="empty">Loading…</p></div>;
  }
  if (boot.phase === "error") {
    return (
      <div className="login-screen">
        <div className="login-card">
          <div className="brand"><span className="brand-mark">◆</span><span className="brand-name">Overseer</span></div>
          <p className="login-error">Configuration unavailable: {boot.message}</p>
        </div>
      </div>
    );
  }

  const { supabase } = boot;
  if (!session) {
    return <Login supabase={supabase} />;
  }
  return (
    <Dashboard
      supabase={supabase}
      ownerEmail={session.user.email ?? "owner"}
      onSignOut={() => void supabase.auth.signOut()}
    />
  );
}
