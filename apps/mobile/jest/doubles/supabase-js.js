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
