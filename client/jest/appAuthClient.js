function hasAuthClient(auth) {
  try {
    return auth.supabase.auth !== undefined;
  } catch {
    return false;
  }
}

function keychainStorage() {
  const SecureStore = require('expo-secure-store');
  return {
    getItem: (key) => SecureStore.getItemAsync(key).catch(() => null),
    setItem: (key, value) => SecureStore.setItemAsync(key, value),
    removeItem: (key) => SecureStore.deleteItemAsync(key),
  };
}

function appLikeAuthClient() {
  const { createClient } = require('@supabase/supabase-js');
  const url = process.env.EXPO_PUBLIC_SUPABASE_URL;
  const storage = keychainStorage();
  const client = createClient(url, process.env.EXPO_PUBLIC_SUPABASE_ANON_KEY, {
    auth: {
      storage,
      persistSession: true,
      autoRefreshToken: true,
      detectSessionInUrl: false,
      flowType: 'pkce',
    },
  });
  const storageKey = `sb-${new URL(url).hostname.replace(/\..*$/, '')}-auth-token`;
  return { auth: client.auth, clearPersistedSession: () => storage.removeItem(storageKey) };
}

function ensureAppAuthClient() {
  const auth = require('../src/shared/auth');
  if (typeof auth.installAuthClient !== 'function' || hasAuthClient(auth)) return;
  auth.installAuthClient(appLikeAuthClient());
}

module.exports = { ensureAppAuthClient };
