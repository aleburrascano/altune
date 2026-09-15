import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js';

import { apiBase } from '@shared/api-client';

import { supabase } from './supabaseClient';

// NON-PRODUCTION test-auth path for the mobile app (see
// docs/webauth-testing-design.md). It mints a session for the single dedicated
// test user by calling go-api's non-prod `POST /test/login`, then injects that
// session into the Supabase client's own storage so `useSession` observes a
// live session and authed screens render — without the web OAuth flow that is
// broken on Expo web.
//
// This is an intentional auth bypass on the client side; the crux that keeps it
// safe is a single build-time guard: it is enabled ONLY in a development build
// (`__DEV__`) that also opts in with `EXPO_PUBLIC_TEST_AUTH=1`. A production
// build has `__DEV__ === false`, so `isTestAuthEnabled()` is false and Metro
// strips the whole path as dead code — the endpoint is never called and no
// token is ever injected. The token itself only authenticates as the go-api
// test user, never a real account.

const TEST_LOGIN_PATH = '/test/login';

// The test verifier in go-api never issues (nor needs) a refresh token; the
// Supabase client only requires a non-empty value for the session to be
// considered valid. A real refresh is never attempted within a token's lifetime.
const TEST_REFRESH_TOKEN = 'test-auth-no-refresh';

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
  _notifyAllSubscribers?: (event: AuthChangeEvent, session: Session) => Promise<void>;
};

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

/** Builds the Supabase Session object the client stores for the test user. */
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
    expires_at: login.expires_at,
    expires_in: Math.max(0, login.expires_at - nowSeconds),
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
  await store.storage.setItem(store.storageKey, JSON.stringify(session));
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
