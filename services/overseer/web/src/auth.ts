import { createClient, type SupabaseClient } from "@supabase/supabase-js";
import type { ClientConfig } from "./config";

export function createSupabase(cfg: ClientConfig): SupabaseClient {
  return createClient(cfg.supabaseUrl, cfg.supabaseAnonKey, {
    auth: {
      persistSession: true,
      autoRefreshToken: true,
      storageKey: "overseer-supabase-auth",
    },
  });
}

export async function accessToken(supabase: SupabaseClient): Promise<string | null> {
  const { data } = await supabase.auth.getSession();
  return data.session?.access_token ?? null;
}

export async function refreshedToken(supabase: SupabaseClient): Promise<string | null> {
  const { data, error } = await supabase.auth.refreshSession();
  if (error) return null;
  return data.session?.access_token ?? null;
}
