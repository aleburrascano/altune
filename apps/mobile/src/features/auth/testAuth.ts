import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js';

import { apiBase } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';

const TEST_LOGIN_PATH = '/test/login';

// The test verifier in go-api never issues (nor needs) a refresh token; the
// Supabase client only requires a non-empty value for the session to be
// considered valid. The stub value below can never be redeemed with Supabase,
// so auto-refresh MUST stay off for the injected session (see
// injectTestSession) — otherwise, when the client believes the token is about
// to expire, it POSTs this stub to Supabase, which rejects it and silently
// drops the test session mid-run.
const TEST_REFRESH_TOKEN = 'test-auth-no-refresh';

// The injected session is a NON-EXPIRING stub. go-api's test token itself is
// short-lived (1h), but the Supabase *session* the client tracks is what
// decides whether `useSession` stays signed-in and whether the SDK schedules a
// refresh. Pinning the session's expiry far into the future keeps the client
// from ever treating it as expired and from attempting the (unredeemable)
// refresh, so a harness driving screens does not lose its session partway
// through a run. A fixed, obviously-sentinel far-future instant (2100-01-01Z).
const NON_EXPIRING_AT_SECONDS = Math.floor(Date.UTC(2100, 0, 1) / 1000);

// A minimal, stable read of the Supabase auth client's session storage. The
// client owns both the concrete storage adapter (in-memory on web, SecureStore
// on native) and the storage key derived from the project URL, so injecting
// through them keeps the app's real read path intact. `_notifyAllSubscribers`
// lets an already-mounted `useSession` flip to signed-in immediately rather than
// waiting for a reload; it is optional so a client version without it still
// works via the next `getSession()`.
type SessionStore = {
  storageKey: string;
  storage: { setItem: (key: string, value: string) => Promise<void> };
  // Public GoTrue API: stops the auto-refresh ticker so the client never POSTs
  // the unredeemable stub refresh token. Optional so an older client without it
  // still works (a non-expiring session already avoids scheduling a refresh).
  stopAutoRefresh?: () => unknown;
  _notifyAllSubscribers?: (event: AuthChangeEvent, session: Session) => Promise<void>;
};

/**
 * Fails loudly and diagnosably if a Supabase SDK upgrade changes the auth
 * session-store internals the injection path depends on. Without this, a shape
 * change degrades to a silent 'authed screen never renders'; the thrown error
 * names exactly what moved so the harness fix is obvious. Never a security
 * regression — this whole path is stripped from production builds.
 */
function assertSessionStoreShape(store: SessionStore): void {
  const hasKey = typeof store.storageKey === 'string' && store.storageKey.length > 0;
  const hasStorage = typeof store.storage?.setItem === 'function';
  if (!hasKey || !hasStorage) {
    throw new Error(
      'test-auth: Supabase auth session-store internals changed ' +
        '(expected string `storageKey` and `storage.setItem`); the injection ' +
        'path must be updated for this @supabase/supabase-js version',
    );
  }
}

/**
 * The one guard that makes this path non-production-only. True only in a dev
 * build that has explicitly opted in. Read at call time so it cannot be cached
 * past a build boundary.
 */
export function isTestAuthEnabled(): boolean {
  return __DEV__ === true && process.env.EXPO_PUBLIC_TEST_AUTH === '1';
}

function assertTestAuthEnabled(): void {
  if (!isTestAuthEnabled()) {
    throw new Error(
      'test-auth is disabled: available only in a non-production build with EXPO_PUBLIC_TEST_AUTH=1',
    );
  }
}

export interface TestLoginResponse {
  access_token: string;
  token_type: string;
  expires_at: number;
  user_id: string;
}

/** Calls go-api's non-prod `POST /test/login` and returns the issued token. */
export async function fetchTestLoginToken(): Promise<TestLoginResponse> {
  assertTestAuthEnabled();
  const url = `${apiBase}${TEST_LOGIN_PATH}`;
  const response = await fetch(url, { method: 'POST' });
  if (!response.ok) {
    throw new Error(`test-login failed: ${url} returned ${response.status}`);
  }
  const body = (await response.json()) as Partial<TestLoginResponse>;
  if (
    typeof body.access_token !== 'string' ||
    typeof body.user_id !== 'string' ||
    typeof body.expires_at !== 'number'
  ) {
    throw new Error('test-login returned a malformed token payload');
  }
  return {
    access_token: body.access_token,
    token_type: typeof body.token_type === 'string' ? body.token_type : 'bearer',
    expires_at: body.expires_at,
    user_id: body.user_id,
  };
}

/**
 * Builds the Supabase Session object the client stores for the test user. The
 * session is deliberately NON-EXPIRING (`expires_at` far in the future),
 * independent of the go-api token's own 1h lifetime, so the client neither
 * auto-expires the session nor attempts to refresh the unredeemable stub token
 * mid-run. Auto-refresh is additionally stopped in injectTestSession.
 */
export function buildTestSession(login: TestLoginResponse): Session {
  const nowSeconds = Math.floor(Date.now() / 1000);
  const user: User = {
    id: login.user_id,
    app_metadata: {},
    user_metadata: {},
    aud: 'authenticated',
    created_at: new Date(0).toISOString(),
  };
  return {
    access_token: login.access_token,
    refresh_token: TEST_REFRESH_TOKEN,
    token_type: 'bearer',
    expires_at: NON_EXPIRING_AT_SECONDS,
    expires_in: Math.max(0, NON_EXPIRING_AT_SECONDS - nowSeconds),
    user,
  };
}

/**
 * Writes the session into the Supabase client's storage and notifies any live
 * listener, so `useSession` sees a signed-in session through the app's real
 * read path.
 */
export async function injectTestSession(session: Session): Promise<void> {
  assertTestAuthEnabled();
  const store = supabase.auth as unknown as SessionStore;
  assertSessionStoreShape(store);
  await store.storage.setItem(store.storageKey, JSON.stringify(session));
  // Stop the auto-refresh ticker via the public GoTrue API so the client never
  // POSTs the unredeemable stub refresh token and silently drops the session.
  if (typeof store.stopAutoRefresh === 'function') {
    void store.stopAutoRefresh();
  }
  if (typeof store._notifyAllSubscribers === 'function') {
    await store._notifyAllSubscribers('SIGNED_IN', session);
  }
}

/**
 * End-to-end test-auth bootstrap: fetch a token for the test user and inject it.
 * A no-op returning false when the guard is off, so it is safe to call
 * unconditionally from app startup.
 */
export async function bootstrapTestAuth(): Promise<boolean> {
  if (!isTestAuthEnabled()) {
    return false;
  }
  const login = await fetchTestLoginToken();
  await injectTestSession(buildTestSession(login));
  return true;
}
