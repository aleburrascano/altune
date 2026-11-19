import { createElement, type ReactElement, type ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth';

import { _resetLockoutsForTest, AuthPortProvider } from '@features/signIn';
import type { AuthPort } from '@features/signIn';

const { ensureAppAuthClient } = require('@testing/appAuthClient.js') as {
  ensureAppAuthClient: () => void;
};

type SupabaseAuthClient = typeof supabase.auth;

export function createSupabaseAuthMock<M extends keyof SupabaseAuthClient>(
  ...methods: M[]
): Record<M, jest.Mock> {
  const mocked = methods.map((method) => [method, jest.fn()] as const);
  const mockedByMethod = Object.fromEntries(mocked) as Record<M, jest.Mock>;
  ensureAppAuthClient();
  Object.assign(supabase.auth, mockedByMethod);
  beforeEach(() => {
    for (const [, mock] of mocked) mock.mockReset();
    _resetLockoutsForTest();
  });
  return mockedByMethod;
}

export type AuthPortMock = {
  [K in keyof AuthPort]: jest.Mock<ReturnType<AuthPort[K]>, Parameters<AuthPort[K]>>;
};

export function createAuthPortMock(): AuthPortMock {
  return {
    reportSignInFailure: jest.fn(),
  };
}

interface AuthPortWrapperProps {
  children: ReactNode;
}

export function authPortWrapper(port: AuthPort = createAuthPortMock()) {
  return function AuthPortWrapper({ children }: AuthPortWrapperProps): ReactElement {
    const props = { port, children };
    return createElement(AuthPortProvider, props);
  };
}

export async function runAsyncAuthHook<H extends { state: unknown }>(
  useHook: () => H,
  invoke: (hook: H) => Promise<void>,
  port: AuthPort = createAuthPortMock(),
): Promise<H['state']> {
  const { result } = renderHook(useHook, { wrapper: authPortWrapper(port) });
  await act(async () => {
    await invoke(result.current);
  });
  return result.current.state;
}
