// Test double for @supabase/supabase-js used by testAuth.test.tsx.
//
// The real client cannot be constructed under Node 20 in jest (its realtime
// client needs a global WebSocket), so createClient is faked here. The fake
// KEEPS the real storage adapter the app configures (the native SecureStore
// double in tests) and reads it back exactly as GoTrue does: JSON parse, an
// access/refresh/expires_at shape check, and the same 90s expiry margin. So the
// injection path is exercised against the real store, and getSession /
// onAuthStateChange behave like the SDK.

const STORAGE_KEY = 'sb-fixture-auth-token';
const EXPIRY_MARGIN_MS = 90_000;

function createClient(_url, _key, options) {
  const storage = options.auth.storage;
  const listeners = new Set();

  const auth = {
    storage,
    storageKey: STORAGE_KEY,
    async getSession() {
      const raw = await storage.getItem(STORAGE_KEY);
      if (raw == null) return { data: { session: null }, error: null };
      const session = JSON.parse(raw);
      const valid =
        typeof session === 'object' &&
        session !== null &&
        'access_token' in session &&
        'refresh_token' in session &&
        'expires_at' in session;
      const expired = session.expires_at
        ? session.expires_at * 1000 - Date.now() < EXPIRY_MARGIN_MS
        : false;
      if (!valid || expired) return { data: { session: null }, error: null };
      return { data: { session }, error: null };
    },
    onAuthStateChange(cb) {
      listeners.add(cb);
      return { data: { subscription: { unsubscribe: () => listeners.delete(cb) } } };
    },
    async _notifyAllSubscribers(event, session) {
      for (const cb of [...listeners]) cb(event, session);
    },
    stopAutoRefresh() {},
  };

  return { auth };
}

module.exports = { createClient };
