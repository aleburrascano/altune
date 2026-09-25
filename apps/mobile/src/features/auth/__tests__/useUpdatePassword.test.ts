import { act } from '@testing-library/react-native';

import { AUTH_ACTION_TIMEOUT_MS } from '../authDeadline';
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

describe('a server rate limit', () => {
  const RATE_LIMITED = {
    data: null,
    error: { name: 'AuthApiError', status: 429, code: 'over_email_send_rate_limit', message: 'limit' },
  };

  const RATE_LIMITED_BY_CODE = {
    data: null,
    error: { name: 'AuthApiError', status: 400, code: 'over_request_rate_limit', message: 'limit' },
  };

  const UNAVAILABLE = {
    data: null,
    error: { name: 'AuthApiError', status: 503, message: 'down' },
  };

  const RETRYABLE_429 = {
    data: null,
    error: { name: 'AuthRetryableFetchError', status: 429, message: 'limit' },
  };

  describe.each([
    [
      'update-password',
      updateUser,
      () => runAsyncAuthHook(useUpdatePassword, (hook) => hook.updatePassword('pw')),
    ],
  ] as const)('%s: a server rate limit is not a network error', (_name, mock, run) => {
    it('maps a resolved 429 to too_many_attempts', async () => {
      mock.mockResolvedValue(RATE_LIMITED);

      expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    });

    it('maps an over_*_rate_limit code on another status to too_many_attempts', async () => {
      mock.mockResolvedValue(RATE_LIMITED_BY_CODE);

      expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    });

    it('maps a 429 the SDK wrapped as a retryable fetch error to too_many_attempts', async () => {
      mock.mockResolvedValue(RETRYABLE_429);

      expect(await run()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    });

    it('still maps a 503 to network', async () => {
      mock.mockResolvedValue(UNAVAILABLE);

      expect(await run()).toEqual({ kind: 'error', reason: 'network' });
    });
  });
});
