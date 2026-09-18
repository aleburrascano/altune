import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

type SupabaseAuthClient = typeof supabase.auth;

/**
 * Jest hoists every `jest.mock` factory above this module, so a suite declares
 * the empty shell itself — `jest.mock('@shared/auth/supabaseClient', () => ({
 * supabase: { auth: {} } }))` — and calls this to fill in the methods it drives.
 * Each mock is reset before every test, so no test inherits a queued resolution
 * from the one before it.
 */
export function createSupabaseAuthMock<M extends keyof SupabaseAuthClient>(
  ...methods: M[]
): Record<M, jest.Mock> {
  const mocked = methods.map((method) => [method, jest.fn()] as const);
  const mockedByMethod = Object.fromEntries(mocked) as Record<M, jest.Mock>;
  Object.assign(supabase.auth, mockedByMethod);
  beforeEach(() => {
    for (const [, mock] of mocked) mock.mockReset();
  });
  return mockedByMethod;
}

/**
 * The `act` wrapper is what makes the returned state the terminal one the action
 * settled on rather than the `pending` React had rendered when it was read.
 */
export async function runAsyncAuthHook<H extends { state: unknown }>(
  useHook: () => H,
  invoke: (hook: H) => Promise<void>,
): Promise<H['state']> {
  const { result } = renderHook(useHook);
  await act(async () => {
    await invoke(result.current);
  });
  return result.current.state;
}
