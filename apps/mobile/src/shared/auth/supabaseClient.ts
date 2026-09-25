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

// Older web builds wrote the session to plaintext localStorage. Delete that copy
// whenever the SDK touches its key. Reaching localStorage can throw (blocked
// storage, sandboxed iframe) and must never break sign-in.
function scrubLegacyLocalStorage(key: string): void {
  try {
    if (typeof window !== 'undefined') window.localStorage?.removeItem(key);
  } catch {
    // Nothing persisted that we can reach; nothing to scrub.
  }
}

// Web session storage lives in page memory only (#945). localStorage has no
// encryption at rest and is readable by any script in the origin, so the
// access/refresh token pair must never be written there. The trade-off: a page
// reload signs the user out on web. Native keeps its SecureStore persistence.
function createInMemoryWebStorage(): AuthStorage {
  const memory = new Map<string, string>();
  return {
    getItem: (key) => {
      scrubLegacyLocalStorage(key);
      return Promise.resolve(memory.get(key) ?? null);
    },
    setItem: (key, value) => {
      scrubLegacyLocalStorage(key);
      memory.set(key, value);
      return Promise.resolve();
    },
    removeItem: (key) => {
      scrubLegacyLocalStorage(key);
      memory.delete(key);
      return Promise.resolve();
    },
  };
}

const KEYCHAIN_OPTS = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };

const secureStoreAdapter: AuthStorage =
  Platform.OS === 'web'
    ? createInMemoryWebStorage()
    : {
        getItem: (key: string): Promise<string | null> =>
          SecureStore.getItemAsync(key, KEYCHAIN_OPTS).catch(() => null),
        setItem: (key: string, value: string): Promise<void> =>
          SecureStore.setItemAsync(key, value, KEYCHAIN_OPTS),
        removeItem: (key: string): Promise<void> => SecureStore.deleteItemAsync(key, KEYCHAIN_OPTS),
      };

export const supabase: SupabaseClient = createClient(SUPABASE_URL, SUPABASE_ANON_KEY, {
  auth: {
    storage: secureStoreAdapter,
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
