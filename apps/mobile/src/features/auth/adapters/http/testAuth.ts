import type { AuthChangeEvent, Session, User } from '@supabase/supabase-js';

import { apiBase } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { isTestAuthEnabled, markTestAuthBootSettled } from '@shared/auth/testAuthBoot';

export { isTestAuthEnabled };

const TEST_LOGIN_PATH = '/test/login';

const TEST_REFRESH_TOKEN = 'test-auth-no-refresh';

const NON_EXPIRING_AT_SECONDS = Math.floor(Date.UTC(2100, 0, 1) / 1000);

type SessionStore = {
  storageKey: string;
  storage: { setItem: (key: string, value: string) => Promise<void> };
  stopAutoRefresh?: () => unknown;
  _notifyAllSubscribers?: (event: AuthChangeEvent, session: Session) => Promise<void>;
};

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

export async function injectTestSession(session: Session): Promise<void> {
  assertTestAuthEnabled();
  const store = supabase.auth as unknown as SessionStore;
  assertSessionStoreShape(store);
  await store.storage.setItem(store.storageKey, JSON.stringify(session));
  if (typeof store.stopAutoRefresh === 'function') {
    void store.stopAutoRefresh();
  }
  if (typeof store._notifyAllSubscribers === 'function') {
    await store._notifyAllSubscribers('SIGNED_IN', session);
  }
}

export async function bootstrapTestAuth(): Promise<boolean> {
  if (!isTestAuthEnabled()) {
    return false;
  }
  try {
    const login = await fetchTestLoginToken();
    await injectTestSession(buildTestSession(login));
    return true;
  } finally {
    markTestAuthBootSettled();
  }
}
