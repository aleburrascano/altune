import { createElement, type ReactElement, type ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { _resetLockoutsForTest } from '@features/auth/domain/attemptLockout';
import { AuthPortProvider } from '@features/auth/ports/AuthPortProvider';
import type { AuthPort } from '@features/auth/ports/authPort';

type SupabaseAuthClient = typeof supabase.auth;

export function createSupabaseAuthMock<M extends keyof SupabaseAuthClient>(
  ...methods: M[]
): Record<M, jest.Mock> {
  const mocked = methods.map((method) => [method, jest.fn()] as const);
  const mockedByMethod = Object.fromEntries(mocked) as Record<M, jest.Mock>;
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
    isTestAuthEnabled: jest.fn(() => false),
    bootstrapTestAuth: jest.fn(() => Promise.resolve(true)),
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
