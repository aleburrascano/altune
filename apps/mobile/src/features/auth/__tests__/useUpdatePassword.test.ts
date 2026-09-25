import { useUpdatePassword } from '../hooks/useUpdatePassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { updateUser, signOut } = createSupabaseAuthMock('updateUser', 'signOut');

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

  it('signs out the other sessions once after a successful update', async () => {
    updateUser.mockResolvedValue({ data: { user: {} }, error: null });
    signOut.mockResolvedValue({ error: null });

    await updatePassword();

    expect(signOut).toHaveBeenCalledTimes(1);
    expect(signOut).toHaveBeenCalledWith({ scope: 'others' });
  });

  it('never signs out on an update error', async () => {
    updateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    await updatePassword();

    expect(signOut).not.toHaveBeenCalled();
  });

  it('stays ok when the other-session signOut resolves an error', async () => {
    updateUser.mockResolvedValue({ data: { user: {} }, error: null });
    signOut.mockResolvedValue({ error: { name: 'AuthApiError', status: 500, message: 'boom' } });

    expect(await updatePassword()).toEqual({ kind: 'ok' });
  });

  it('stays ok when the other-session signOut rejects', async () => {
    updateUser.mockResolvedValue({ data: { user: {} }, error: null });
    signOut.mockRejectedValue(new Error('offline'));

    expect(await updatePassword()).toEqual({ kind: 'ok' });
  });
});
