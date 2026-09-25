import { useSignIn } from '../hooks/useSignIn';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { signInWithPassword } = createSupabaseAuthMock('signInWithPassword');

describe('useSignIn: a GoTrue code this app has not been taught', () => {
  it('maps an unrecognised 400 code to unknown, not invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'some_future_code', message: 'New rejection' },
    });

    expect(await runAsyncAuthHook(useSignIn, (hook) => hook.signIn('a@b.co', 'pw'))).toEqual({
      kind: 'error',
      reason: 'unknown',
    });
  });
});
