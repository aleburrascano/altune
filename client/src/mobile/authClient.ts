import { createClient } from '@supabase/supabase-js';

import type { AuthClient } from '@shared/auth';
import { fetchWithinAuthDeadline } from '@/mobile/authFetch';
import type { AuthFetch } from '@/mobile/authFetch';
import { createAuthStorage } from '@/mobile/authStorage';
import type { AuthStorage } from '@/mobile/authStorage';

export type AuthClientConfig = { url: string | undefined; anonKey: string | undefined };

export type AuthClientParts = { storage: AuthStorage; fetch: AuthFetch };

function requiredEnv(value: string | undefined, name: string): string {
  if (value == null || value === '') {
    throw new Error(`Missing required environment variable ${name}`);
  }
  return value;
}

function appConfig(): AuthClientConfig {
  return {
    url: process.env.EXPO_PUBLIC_SUPABASE_URL,
    anonKey: process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY,
  };
}

function appParts(): AuthClientParts {
  return { storage: createAuthStorage(), fetch: (input, init) => fetch(input, init) };
}

const FIRST_DOT_ONWARD = /\..*$/;

function sessionStorageKey(url: string): string {
  return `sb-${new URL(url).hostname.replace(FIRST_DOT_ONWARD, '')}-auth-token`;
}

const SESSION_POLICY = {
  persistSession: true,
  autoRefreshToken: true,
  detectSessionInUrl: false,
  flowType: 'pkce',
} as const;

function supabaseAuth(url: string, anonKey: string, parts: AuthClientParts): AuthClient['auth'] {
  const auth = { ...SESSION_POLICY, storage: parts.storage };
  const transport = { fetch: fetchWithinAuthDeadline(parts.fetch) };
  return createClient(url, anonKey, { auth, global: transport }).auth;
}

export function createAuthClient(
  config: AuthClientConfig = appConfig(),
  parts: AuthClientParts = appParts(),
): AuthClient {
  const url = requiredEnv(config.url, 'EXPO_PUBLIC_SUPABASE_URL');
  const anonKey = requiredEnv(config.anonKey, 'EXPO_PUBLIC_SUPABASE_ANON_KEY');
  const storageKey = sessionStorageKey(url);
  const auth = supabaseAuth(url, anonKey, parts);
  return { auth, clearPersistedSession: () => parts.storage.removeItem(storageKey) };
}
