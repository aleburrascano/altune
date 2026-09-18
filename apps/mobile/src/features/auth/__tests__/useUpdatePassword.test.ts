import { useUpdatePassword } from '../hooks/useUpdatePassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { updateUser } = createSupabaseAuthMock('updateUser');

const updatePassword = () =>
  runAsyncAuthHook(useUpdatePassword, (hook) => hook.updatePassword('new-password'));

describe('useUpdatePassword: mapping the resolved { error } of updateUser', () => {
  it('maps a swallowed AuthRetryableFetchError to network, not unknown', async () => {
    updateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps an AuthWeakPasswordError to weak_password', async () => {
    updateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthWeakPasswordError', status: 422, code: 'weak_password', message: 'Password is too weak' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'weak_password' });
  });

  it('falls back to unknown for an unrecognised error', async () => {
    updateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports ok on success', async () => {
    updateUser.mockResolvedValue({ data: { user: {} }, error: null });

    expect(await updatePassword()).toEqual({ kind: 'ok' });
  });
});
