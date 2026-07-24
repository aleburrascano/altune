import { Platform } from 'react-native';
import type { SupabaseClient } from '@supabase/supabase-js';
import { createClient } from '@supabase/supabase-js';
import * as SecureStore from 'expo-secure-store';

const SUPABASE_URL = process.env.EXPO_PUBLIC_SUPABASE_URL ?? '';
const SUPABASE_ANON_KEY = process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY ?? '';

const webStorage =
  typeof window !== 'undefined' && window.localStorage != null
    ? {
        getItem: (key: string): Promise<string | null> =>
          Promise.resolve(window.localStorage.getItem(key)),
        setItem: (key: string, value: string): Promise<void> => {
          window.localStorage.setItem(key, value);
          return Promise.resolve();
        },
        removeItem: (key: string): Promise<void> => {
          window.localStorage.removeItem(key);
          return Promise.resolve();
        },
      }
    : {
        getItem: (_key: string): Promise<string | null> => Promise.resolve(null),
        setItem: (_key: string, _value: string): Promise<void> => Promise.resolve(),
        removeItem: (_key: string): Promise<void> => Promise.resolve(),
      };

const KEYCHAIN_OPTS = { keychainAccessible: SecureStore.AFTER_FIRST_UNLOCK };

const secureStoreAdapter =
  Platform.OS === 'web'
    ? webStorage
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
  },
});
