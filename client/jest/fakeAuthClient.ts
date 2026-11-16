import type { AuthChangeEvent, Session } from '@supabase/supabase-js';
import type * as SecureStoreModule from 'expo-secure-store';

import type { AuthClient } from '@shared/auth';

type Auth = AuthClient['auth'];

type SupabaseDouble = {
  createClient(url: string, key: string, options: object): { auth: Auth };
  emitAuthEvent(auth: Auth, event: AuthChangeEvent, session: Session | null): Promise<void>;
};

export type FakeAuthClient = AuthClient & {
  emit(event: AuthChangeEvent, session: Session | null): Promise<void>;
};

const STORAGE_KEY = 'sb-fixture-auth-token';

export function fakeAuthClient(): FakeAuthClient {
  const SecureStore = require('expo-secure-store') as typeof SecureStoreModule;
  const double = require('./doubles/supabase-js.js') as SupabaseDouble;
  const keychain = {
    getItem: (key: string) => SecureStore.getItemAsync(key),
    setItem: (key: string, value: string) => SecureStore.setItemAsync(key, value),
    removeItem: (key: string) => SecureStore.deleteItemAsync(key),
  };
  const { auth } = double.createClient('https://fixture.supabase.co', 'fixture-anon-key', {
    auth: { storage: keychain },
  });
  return {
    auth,
    clearPersistedSession: () => keychain.removeItem(STORAGE_KEY),
    emit: (event, session) => double.emitAuthEvent(auth, event, session),
  };
}

export type LooseAuth = { [K in keyof Auth]?: unknown };

export function authClientOver(auth: LooseAuth): AuthClient {
  return { auth: auth as Auth, clearPersistedSession: () => Promise.resolve() };
}
