import { Platform } from 'react-native';
import type { SupabaseClient } from '@supabase/supabase-js';
import { createClient } from '@supabase/supabase-js';
import * as SecureStore from 'expo-secure-store';

import { fetchWithinAuthDeadline } from './authDeadline';

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

type AuthStorage = {
  getItem: (key: string) => Promise<string | null>;
  setItem: (key: string, value: string) => Promise<void>;
  removeItem: (key: string) => Promise<void>;
};

function reachableLocalStorage(): Storage | null {
  if (typeof window === 'undefined') return null;
  try {
    return window.localStorage ?? null;
  } catch {
    return null;
  }
}

function readWebStorage(fallback: Map<string, string>, key: string): Promise<string | null> {
  if (fallback.has(key)) return Promise.resolve(fallback.get(key) ?? null);
  const localStorage = reachableLocalStorage();
  if (localStorage == null) return Promise.resolve(null);
  try {
    return Promise.resolve(localStorage.getItem(key));
  } catch {
    return Promise.resolve(null);
  }
}

function trySetItem(localStorage: Storage, key: string, value: string): boolean {
  try {
    localStorage.setItem(key, value);
    return true;
  } catch {
    return false;
  }
}

function writeWebStorage(fallback: Map<string, string>, key: string, value: string): Promise<void> {
  const localStorage = reachableLocalStorage();
  if (localStorage == null || !trySetItem(localStorage, key, value)) {
    fallback.set(key, value);
    return Promise.resolve();
  }
  fallback.delete(key);
  return Promise.resolve();
}

function tryRemoveItem(localStorage: Storage, key: string): void {
  try {
    localStorage.removeItem(key);
  } catch {
    return;
  }
}

function deleteFromWebStorage(fallback: Map<string, string>, key: string): Promise<void> {
  fallback.delete(key);
  const localStorage = reachableLocalStorage();
  if (localStorage != null) tryRemoveItem(localStorage, key);
  return Promise.resolve();
}

export function createLocalStorageWebStorage(): AuthStorage {
  const unreachableFallback = new Map<string, string>();

  return {
    getItem: (key) => readWebStorage(unreachableFallback, key),
    setItem: (key, value) => writeWebStorage(unreachableFallback, key, value),
    removeItem: (key) => deleteFromWebStorage(unreachableFallback, key),
  };
}

const KEYCHAIN_OPTS = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };

const authStorage: AuthStorage =
  Platform.OS === 'web'
    ? createLocalStorageWebStorage()
    : {
        getItem: (key: string): Promise<string | null> =>
          SecureStore.getItemAsync(key, KEYCHAIN_OPTS).catch(() => null),
        setItem: (key: string, value: string): Promise<void> =>
          SecureStore.setItemAsync(key, value, KEYCHAIN_OPTS),
        removeItem: (key: string): Promise<void> => SecureStore.deleteItemAsync(key, KEYCHAIN_OPTS),
      };

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
    // PKCE keeps the OAuth `altune://auth/callback` redirect carrying only a
    // single-use `code` — never a live access/refresh token pair. `altune` is a
    // bare custom scheme with no App/Universal Link verification, so an implicit
    // grant would let another app intercept the redirect and replay real
    // tokens; a `code` is worthless without the verifier we hold (see #655).
    flowType: 'pkce',
  },
  global: { fetch: fetchWithinAuthDeadline },
});
