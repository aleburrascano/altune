import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook } from '@testing-library/react-native';

import {
  bootstrapTestAuth,
  buildTestSession,
  fetchTestLoginToken,
  injectTestSession,
  isTestAuthEnabled,
  type TestLoginResponse,
} from '../testAuth';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';

// The real @supabase/supabase-js client cannot be constructed under Node 20 in
// jest (its realtime client needs a global WebSocket), so — as
// supabaseClient.test.ts does — createClient is faked. jest hoists this mock
// above the imports above. The double keeps the REAL storage adapter the app
// configures (native SecureStore double here) and reads it back exactly as
// GoTrue does, so injectTestSession is exercised against the real store and
// getSession/onAuthStateChange behave like the SDK.
jest.mock('@supabase/supabase-js', () => require('../../../../jest/doubles/supabase-js.js'));

const { __http } = require('../../../../jest/doubles/fetch.js') as {
  __http: {
    reply(spec: string, response: { status?: number; json?: unknown }): unknown;
  };
};

const TEST_USER_ID = '11111111-1111-1111-1111-111111111111';

function loginResponse(overrides: Partial<TestLoginResponse> = {}): TestLoginResponse {
  return {
    access_token: 'test.jwt.token',
    token_type: 'bearer',
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    user_id: TEST_USER_ID,
    ...overrides,
  };
}

function replyWithLogin(body: unknown): void {
  __http.reply('POST /test/login', { status: 200, json: body });
}

const devFlag = globalThis as unknown as { __DEV__: boolean };

function withDev(value: boolean, fn: () => void): void {
  const original = devFlag.__DEV__;
  devFlag.__DEV__ = value;
  try {
    fn();
  } finally {
    devFlag.__DEV__ = original;
  }
}

async function flush(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
    await Promise.resolve();
    await Promise.resolve();
  });
}

beforeEach(() => {
  process.env.EXPO_PUBLIC_TEST_AUTH = '1';
});

afterEach(() => {
  delete process.env.EXPO_PUBLIC_TEST_AUTH;
});

describe('isTestAuthEnabled — the non-prod guard', () => {
  it('is enabled only in a dev build that opted in with EXPO_PUBLIC_TEST_AUTH=1', () => {
    withDev(true, () => {
      expect(isTestAuthEnabled()).toBe(true);
    });
  });

  it('is disabled when the opt-in flag is absent, even in a dev build', () => {
    delete process.env.EXPO_PUBLIC_TEST_AUTH;
    withDev(true, () => {
      expect(isTestAuthEnabled()).toBe(false);
    });
  });

  it('is disabled in a production build (__DEV__ false) regardless of the flag', () => {
    withDev(false, () => {
      expect(isTestAuthEnabled()).toBe(false);
    });
  });
});

describe('fetchTestLoginToken — calling go-api /test/login', () => {
  it('returns the issued token on a 200', async () => {
    replyWithLogin(loginResponse({ access_token: 'issued.jwt' }));

    await expect(fetchTestLoginToken()).resolves.toMatchObject({
      access_token: 'issued.jwt',
      user_id: TEST_USER_ID,
      token_type: 'bearer',
    });
  });

  it('defaults an absent token_type to bearer', async () => {
    const full = loginResponse();
    replyWithLogin({
      access_token: full.access_token,
      expires_at: full.expires_at,
      user_id: full.user_id,
    });

    await expect(fetchTestLoginToken()).resolves.toMatchObject({ token_type: 'bearer' });
  });

  it('throws on a non-2xx response', async () => {
    __http.reply('POST /test/login', { status: 404 });

    await expect(fetchTestLoginToken()).rejects.toThrow('returned 404');
  });

  it('throws on a malformed token payload', async () => {
    replyWithLogin({ token_type: 'bearer' });

    await expect(fetchTestLoginToken()).rejects.toThrow('malformed');
  });

  it('refuses to run when the guard is off', async () => {
    delete process.env.EXPO_PUBLIC_TEST_AUTH;

    await expect(fetchTestLoginToken()).rejects.toThrow('disabled');
  });
});

describe('buildTestSession — the Supabase Session for the test user', () => {
  it('maps the token onto a valid session for the dedicated test user', () => {
    const session = buildTestSession(loginResponse({ access_token: 'abc.def' }));

    expect(session.access_token).toBe('abc.def');
    expect(session.user.id).toBe(TEST_USER_ID);
    expect(session.token_type).toBe('bearer');
    expect(session.refresh_token).not.toBe('');
    expect(session.expires_in).toBeGreaterThan(0);
  });

  it('mints a NON-EXPIRING session regardless of the go-api token lifetime', () => {
    // #1395: the injected session must not track the token's short 1h life, or
    // the client auto-expires it / attempts the unredeemable refresh mid-run.
    const nowSeconds = Math.floor(Date.now() / 1000);
    const oneYearSeconds = 365 * 24 * 3600;

    const fromFresh = buildTestSession(loginResponse({ expires_at: nowSeconds + 3600 }));
    const fromExpired = buildTestSession(loginResponse({ expires_at: nowSeconds - 3600 }));

    // Both are pinned far past any realistic run — even an already-expired
    // go-api token yields a live, long-lived session (no negative expires_in).
    expect(fromFresh.expires_at).toBe(fromExpired.expires_at);
    expect(fromFresh.expires_at).toBeGreaterThan(nowSeconds + oneYearSeconds);
    expect(fromExpired.expires_in).toBeGreaterThan(oneYearSeconds);
  });
});

describe('injectTestSession — driving the real Supabase store', () => {
  it('makes supabase.auth.getSession() return a live authed session for the test user', async () => {
    await injectTestSession(buildTestSession(loginResponse({ access_token: 'live.token' })));

    const { data, error } = await supabase.auth.getSession();

    expect(error).toBeNull();
    expect(data.session?.user.id).toBe(TEST_USER_ID);
    expect(data.session?.access_token).toBe('live.token');
  });

  it('keeps the session live long past the 1h go-api token lifetime', async () => {
    // #1395: a harness driving screens for >1h must not see the session vanish.
    await injectTestSession(buildTestSession(loginResponse()));

    const realNow = Date.now();
    const dateSpy = jest.spyOn(Date, 'now').mockReturnValue(realNow + 2 * 3600 * 1000);
    try {
      const { data } = await supabase.auth.getSession();
      expect(data.session?.user.id).toBe(TEST_USER_ID);
    } finally {
      dateSpy.mockRestore();
    }
  });

  it('stops the auto-refresh ticker so the stub refresh token is never POSTed', async () => {
    const stopSpy = jest.spyOn(
      supabase.auth as unknown as { stopAutoRefresh: () => void },
      'stopAutoRefresh',
    );
    try {
      await injectTestSession(buildTestSession(loginResponse()));
      expect(stopSpy).toHaveBeenCalledTimes(1);
    } finally {
      stopSpy.mockRestore();
    }
  });

  it('throws a diagnosable error if the Supabase store internals change shape', async () => {
    const store = supabase.auth as unknown as { storageKey: string };
    const original = store.storageKey;
    store.storageKey = '';
    try {
      await expect(injectTestSession(buildTestSession(loginResponse()))).rejects.toThrow(
        'session-store internals changed',
      );
    } finally {
      store.storageKey = original;
    }
  });

  it('refuses to run when the guard is off', async () => {
    delete process.env.EXPO_PUBLIC_TEST_AUTH;

    await expect(injectTestSession(buildTestSession(loginResponse()))).rejects.toThrow('disabled');
  });
});

describe('the injected token yields an authed session through useSession', () => {
  it('useSession observes signed-in for the test user after the token is injected', async () => {
    await injectTestSession(buildTestSession(loginResponse()));
    const queryClient = new QueryClient();
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );

    const { result, unmount } = renderHook(() => useSession(), { wrapper });
    await flush();

    expect(result.current.status).toBe('signed-in');
    if (result.current.status === 'signed-in') {
      expect(result.current.session.user.id).toBe(TEST_USER_ID);
    }
    unmount();
  });
});

describe('bootstrapTestAuth — the end-to-end startup path', () => {
  it('logs in via /test/login and injects a session getSession() then returns', async () => {
    replyWithLogin(loginResponse({ access_token: 'bootstrapped.token' }));

    await expect(bootstrapTestAuth()).resolves.toBe(true);

    const { data } = await supabase.auth.getSession();
    expect(data.session?.user.id).toBe(TEST_USER_ID);
    expect(data.session?.access_token).toBe('bootstrapped.token');
  });

  it('is a no-op returning false when the guard is off', async () => {
    delete process.env.EXPO_PUBLIC_TEST_AUTH;

    await expect(bootstrapTestAuth()).resolves.toBe(false);
  });
});
