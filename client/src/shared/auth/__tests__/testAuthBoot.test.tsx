import type React from 'react';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import type * as TestAuthBootModule from '@shared/auth/testAuthBoot';

jest.mock('@supabase/supabase-js', () => require('@testing/doubles/supabase-js.js'));

type Notifier = {
  _notifyAllSubscribers: (event: AuthChangeEvent, session: Session | null) => Promise<void>;
};

const globals = globalThis as unknown as { __DEV__: boolean };
const devMode = globals.__DEV__;
const authEnv = process.env.EXPO_PUBLIC_TEST_AUTH;

function setTestAuth(enabled: boolean): void {
  globals.__DEV__ = true;
  if (enabled) process.env.EXPO_PUBLIC_TEST_AUTH = '1';
  else delete process.env.EXPO_PUBLIC_TEST_AUTH;
}

type WrapperProps = { children: React.ReactNode };

const SESSION_SECONDS = 3600;
const MS_PER_SECOND = 1000;

const cleanups: (() => void)[] = [];

function loadFreshModules() {
  jest.resetModules();
  const { QueryClient, QueryClientProvider } = require('@tanstack/react-query');
  const { renderHook, act, cleanup } = require('@testing-library/react-native/pure');
  cleanups.push(cleanup);
  const react = require('react') as typeof React;
  const { useSession } = require('@shared/auth/useSession');
  const { supabase } = require('@shared/auth/supabaseClient');
  const { injectTestSession } = require('@shared/auth/injectTestSession');
  const testAuthBoot = require('@shared/auth/testAuthBoot') as typeof TestAuthBootModule;
  const auth: Notifier = supabase.auth;

  function renderSession() {
    const queryClient = new QueryClient();
    return renderHook(() => useSession(), {
      wrapper: ({ children }: WrapperProps) =>
        react.createElement(QueryClientProvider, { client: queryClient }, children),
    });
  }

  async function flush(): Promise<void> {
    await act(async () => {
      await Promise.resolve();
      await Promise.resolve();
      await Promise.resolve();
    });
  }

  return {
    testAuthBoot,
    renderSession,
    flush,
    act: act as (fn: () => Promise<void> | void) => Promise<void>,
    emit: (event: AuthChangeEvent, session: Session | null) =>
      auth._notifyAllSubscribers(event, session),
    login: (session: Session) => injectTestSession(session) as Promise<void>,
  };
}

function makeSession(userId: string): Session {
  return {
    access_token: `token-${userId}`,
    refresh_token: `refresh-${userId}`,
    expires_in: SESSION_SECONDS,
    expires_at: Math.floor(Date.now() / MS_PER_SECOND) + SESSION_SECONDS,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
  };
}

afterEach(() => {
  cleanups.splice(0).forEach((cleanup) => cleanup());
  globals.__DEV__ = devMode;
  if (authEnv === undefined) delete process.env.EXPO_PUBLIC_TEST_AUTH;
  else process.env.EXPO_PUBLIC_TEST_AUTH = authEnv;
});

describe('useSession while the test login is in flight', () => {
  it('stays loading through the empty initial session and goes straight to signed-in', async () => {
    setTestAuth(true);
    const t = loadFreshModules();
    const { result } = t.renderSession();
    const seen: string[] = [];

    await t.act(() => t.emit('INITIAL_SESSION', null));
    await t.flush();
    seen.push(result.current.status);

    await t.act(() => t.login(makeSession('test-user')));
    seen.push(result.current.status);

    expect(seen).toEqual(['loading', 'signed-in']);
  });

  it('falls back to signed-out once a failed test login settles', async () => {
    setTestAuth(true);
    const t = loadFreshModules();
    const { result } = t.renderSession();

    await t.act(() => t.emit('INITIAL_SESSION', null));
    await t.flush();
    expect(result.current).toEqual({ status: 'loading' });

    t.testAuthBoot.markTestAuthBootSettled();
    await t.flush();

    expect(result.current).toEqual({ status: 'signed-out' });
  });

  it('does not hold anything when test auth is off', async () => {
    setTestAuth(false);
    const t = loadFreshModules();
    const { result } = t.renderSession();

    await t.flush();

    expect(result.current).toEqual({ status: 'signed-out' });
  });
});

describe('the real testAuthBoot module', () => {
  it('holds testAuthBootSettled pending until the boot is marked settled', async () => {
    setTestAuth(true);
    const { testAuthBoot: real } = loadFreshModules();
    let resolved = false;

    const pending = real.testAuthBootSettled().then(() => {
      resolved = true;
    });
    await Promise.resolve();
    await Promise.resolve();
    expect(resolved).toBe(false);

    real.markTestAuthBootSettled();
    await pending;

    expect(resolved).toBe(true);
  });

  it('resolves testAuthBootSettled immediately when test auth is off', async () => {
    setTestAuth(false);
    const { testAuthBoot: real } = loadFreshModules();

    expect(real.isTestAuthEnabled()).toBe(false);
    await expect(real.testAuthBootSettled()).resolves.toBeUndefined();
  });
});
