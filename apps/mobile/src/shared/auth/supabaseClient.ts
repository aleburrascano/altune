import type { SupabaseClient } from '@supabase/supabase-js';
import { createClient } from '@supabase/supabase-js';

import { fetchWithinAuthDeadline } from '@shared/auth/authDeadline';
import { authStorage } from './authStorage';

function requiredEnv(value: string | undefined, name: string): string {
  if (value == null || value === '') {
    throw new Error(`Missing required environment variable ${name}`);
  }
  return value;
}

const SUPABASE_URL = requiredEnv(process.env.EXPO_PUBLIC_SUPABASE_URL, 'EXPO_PUBLIC_SUPABASE_URL');
const SUPABASE_ANON_KEY = requiredEnv(
  process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY,
  'EXPO_PUBLIC_SUPABASE_ANON_KEY',
);

const AUTH_STORAGE_KEY = `sb-${new URL(SUPABASE_URL).hostname.replace(/\..*$/, '')}-auth-token`;

export function clearPersistedAuthSession(): Promise<void> {
  return authStorage.removeItem(AUTH_STORAGE_KEY);
}

export const supabase: SupabaseClient = createClient(SUPABASE_URL, SUPABASE_ANON_KEY, {
  auth: {
    storage: authStorage,
    persistSession: true,
    autoRefreshToken: true,
    detectSessionInUrl: false,
    flowType: 'pkce',
  },
  global: { fetch: fetchWithinAuthDeadline },
});
