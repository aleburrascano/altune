import { act } from '@testing-library/react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
import { useUpdatePassword } from '../hooks/useUpdatePassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { updateUser, signOut } = createSupabaseAuthMock('updateUser', 'signOut');

const updatePassword = () =>
  runAsyncAuthHook(useUpdatePassword, (hook) => hook.updatePassword('new-password'));

describe('useUpdatePassword: the other-session revoke after a committed change', () => {
  let warn: jest.SpyInstance;

  beforeEach(() => {
    updateUser.mockResolvedValue({ data: { user: {} }, error: null });
    warn = jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  });

  afterEach(() => {
    jest.useRealTimers();
    warn.mockRestore();
  });

  it('stays ok when the revoke never settles', async () => {
    jest.useFakeTimers();
    signOut.mockReturnValue(new Promise(() => undefined));

    const state = await updatePassword();
    await act(async () => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS + 1);
    });

    expect(state).toEqual({ kind: 'ok' });
  });

  it('logs a stalled revoke once its own deadline passes', async () => {
    jest.useFakeTimers();
    signOut.mockReturnValue(new Promise(() => undefined));

    await updatePassword();
    await act(async () => {
      jest.advanceTimersByTime(AUTH_ACTION_TIMEOUT_MS);
    });

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('revoking'), {
      name: 'NetworkError',
      message: expect.stringContaining('timeout'),
    });
  });

  it('logs only the projected fields of a resolved revoke error', async () => {
    signOut.mockResolvedValue({
      error: { name: 'AuthApiError', status: 500, code: 'boom', message: 'nope', token: 'secret' },
    });

    await updatePassword();
    await act(async () => undefined);

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('revoking'), {
      name: 'AuthApiError',
      code: 'boom',
      status: 500,
      message: 'nope',
    });
  });

  it('logs a thrown revoke without the raw value', async () => {
    signOut.mockRejectedValue({ token: 'secret' });

    await updatePassword();
    await act(async () => undefined);

    expect(warn).toHaveBeenCalledWith(expect.stringContaining('revoking'), {
      name: 'object',
      message: 'a non-Error value was thrown',
    });
  });
});
