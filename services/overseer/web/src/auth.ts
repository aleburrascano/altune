import { createClient, type SupabaseClient } from "@supabase/supabase-js";
import type { ClientConfig } from "./config";

// createSupabase builds the supabase-js client from the runtime config. It holds
// the session (bearer token + refresh) in memory and refreshes the access token
// automatically; Overseer never sets a cookie of its own — auth is bearer only.
export function createSupabase(cfg: ClientConfig): SupabaseClient {
  return createClient(cfg.supabaseUrl, cfg.supabaseAnonKey, {
    auth: {
      persistSession: true,
      autoRefreshToken: true,
      // Bearer only: the token lives in memory/localStorage under supabase-js's
      // control, never in an Overseer cookie.
      storageKey: "overseer-supabase-auth",
    },
  });
}

// accessToken returns the current session's access token, refreshing it first if
// supabase-js reports it near expiry. Returns null when there is no session.
export async function accessToken(supabase: SupabaseClient): Promise<string | null> {
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token ?? null;
}

// refreshedToken forces a token refresh and returns the new access token, used
// when the API/SSE reports 401 (the token expired mid-session). Returns null when
// the refresh fails (the session is gone and the user must sign in again).
export async function refreshedToken(supabase: SupabaseClient): Promise<string | null> {
  const { data, error } = await supabase.auth.refreshSession();
  if (error) return null;
  return data.session?.access_token ?? null;
}
