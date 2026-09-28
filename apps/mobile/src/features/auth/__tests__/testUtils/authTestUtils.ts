import { act, renderHook } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { _resetLockoutsForTest } from '../../attemptLockout';

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
