/**
 * Supabase client singleton.
 *
 * Per ADR-0006: @supabase/supabase-js is the mobile auth client; session
 * persistence uses expo-secure-store (NOT AsyncStorage — see Risk-#3 in the
 * auth-integration spec). The storage adapter below maps Supabase's
 * synchronous storage interface to expo-secure-store's async API.
 *
 * Env vars (must be set in `.env` / Expo config):
 * - EXPO_PUBLIC_SUPABASE_URL — e.g. https://<project-ref>.supabase.co
 * - EXPO_PUBLIC_SUPABASE_ANON_KEY — the project's anon/publishable key
 *
 * SSR caveat: this module reads `globalThis` / native storage at import time.
 * If altune ever ships a web bundle in SSR mode, the storage adapter needs
 * a guard. v1 is iOS + Android only — not a concern yet.
 *
 * Promoted from features/auth/api/ to shared/auth/ because 2+ features
 * depend on the Supabase client (api-client, playback).
 */
import { createClient, SupabaseClient } from '@supabase/supabase-js';
import * as SecureStore from 'expo-secure-store';

const SUPABASE_URL = process.env.EXPO_PUBLIC_SUPABASE_URL ?? '';
const SUPABASE_ANON_KEY = process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY ?? '';

// AIDEV-NOTE: Supabase SDK calls getItem/setItem/removeItem with the same
// `name` key consistently; the adapter just forwards to SecureStore.
const secureStoreAdapter = {
  getItem: (key: string): Promise<string | null> => SecureStore.getItemAsync(key),
  setItem: (key: string, value: string): Promise<void> => SecureStore.setItemAsync(key, value),
  removeItem: (key: string): Promise<void> => SecureStore.deleteItemAsync(key),
};

export const supabase: SupabaseClient = createClient(SUPABASE_URL, SUPABASE_ANON_KEY, {
  auth: {
    storage: secureStoreAdapter,
    persistSession: true,
    autoRefreshToken: true,
    detectSessionInUrl: false,
  },
});
