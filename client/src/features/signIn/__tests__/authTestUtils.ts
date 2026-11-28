import { createElement, type ReactElement, type ReactNode } from 'react';
import { act, renderHook } from '@testing-library/react-native';

import { authIntentClient, supabase } from '@shared/auth';
import { onSignOut } from '@shared/session';

import { createAttemptLockoutStore } from '@features/signIn/core/attemptLockoutStore';
import { createRecoveryUnlockStore } from '@features/signIn/core/recoveryUnlockStore';
import { createSignInHealth } from '@features/signIn/core/signInHealth';
import { createAuthIntents } from '@features/signIn/shell/completeAuthIntent';
import { SignInProvider } from '@features/signIn/view/SignInProvider';
import type { AuthBrowser, AuthIntents, AuthPort, AuthRouter } from '@features/signIn/ports/ports';
import type { AuthLinkIntent } from '@features/signIn/core/parseAuthLink';

const { ensureAppAuthClient } = require('@testing/appAuthClient.js') as {
  ensureAppAuthClient: () => void;
};

type SupabaseAuthClient = typeof supabase.auth;
type AuthClient = ReturnType<typeof authIntentClient>;

export const recovery = createRecoveryUnlockStore(onSignOut);

let lockout = createAttemptLockoutStore();

function resetLockouts(): void {
  lockout = createAttemptLockoutStore();
}

let authOverride: AuthClient | undefined;
let health = createSignInHealth();
let currentIntents = newIntents();

function newIntents(): AuthIntents {
  return createAuthIntents({
    recovery,
    health,
    authClient: () => authOverride ?? authIntentClient(),
  });
}

export function resetConsumedCredential(): void {
  health = createSignInHealth();
  currentIntents = newIntents();
}

export function flushSignInHealth(): void {
  health.flush();
}

export const intents: AuthIntents = {
  complete: (intent, router) => currentIntents.complete(intent, router),
};

export function completeAuthIntent(intent: AuthLinkIntent, router: AuthRouter, auth: AuthClient) {
  authOverride = auth;
  try {
    return currentIntents.complete(intent, router);
  } finally {
    authOverride = undefined;
  }
}

export function createSupabaseAuthMock<M extends keyof SupabaseAuthClient>(
  ...methods: M[]
): Record<M, jest.Mock> {
  const mocked = methods.map((method) => [method, jest.fn()] as const);
  const mockedByMethod = Object.fromEntries(mocked) as Record<M, jest.Mock>;
  ensureAppAuthClient();
  Object.assign(supabase.auth, mockedByMethod);
  beforeEach(() => {
    for (const [, mock] of mocked) mock.mockReset();
    resetLockouts();
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

export type AuthBrowserMock = {
  [K in keyof AuthBrowser]: jest.Mock<ReturnType<AuthBrowser[K]>, Parameters<AuthBrowser[K]>>;
};

export function createAuthBrowserMock(): AuthBrowserMock {
  return {
    openAuthSession: jest.fn(),
    dismissAuthSession: jest.fn(),
  };
}

interface AuthPortWrapperProps {
  children: ReactNode;
}

export function authPortWrapper(
  port: AuthPort = createAuthPortMock(),
  browser: AuthBrowser = createAuthBrowserMock(),
) {
  return function AuthPortWrapper({ children }: AuthPortWrapperProps): ReactElement {
    const props = {
      port,
      browser,
      lockout,
      recovery,
      intents,
      health,
      children,
    };
    return createElement(SignInProvider, props);
  };
}

export async function runAsyncAuthHook<H extends { state: unknown }>(
  useHook: () => H,
  invoke: (hook: H) => Promise<void>,
  port: AuthPort = createAuthPortMock(),
  browser: AuthBrowser = createAuthBrowserMock(),
): Promise<H['state']> {
  const { result } = renderHook(useHook, { wrapper: authPortWrapper(port, browser) });
  await act(async () => {
    await invoke(result.current);
  });
  return result.current.state;
}
