import { useSignIn } from '../hooks/useSignIn';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { signInWithPassword } = createSupabaseAuthMock('signInWithPassword');

const signIn = () => runAsyncAuthHook(useSignIn, (hook) => hook.signIn('a@b.co', 'pw'));

describe('useSignIn: mapping the resolved { error } of signInWithPassword', () => {
  it('maps a swallowed AuthRetryableFetchError to network, never invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a 503 reachability failure to network', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 503, message: 'Service Unavailable' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a genuine 400 invalid_credentials to invalid_credentials', async () => {
    signInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'invalid_credentials', message: 'Invalid login credentials' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'invalid_credentials' });
  });

  it('reports ok when Supabase returns a session', async () => {
    signInWithPassword.mockResolvedValue({ data: { user: {}, session: {} }, error: null });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });
});
