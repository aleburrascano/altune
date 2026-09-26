import { Platform } from 'react-native';

import { LOCKOUT_AFTER_FAILURES } from '../attemptLockout';
import { useResetPassword } from '../hooks/useResetPassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { resetPasswordForEmail } = createSupabaseAuthMock('resetPasswordForEmail');

const requestResetFor = (email: string) =>
  runAsyncAuthHook(useResetPassword, (hook) => hook.requestReset(email));

const requestReset = () => requestResetFor('a@b.co');

const REJECTED = {
  data: null,
  error: {
    name: 'AuthApiError',
    status: 400,
    code: 'validation_failed',
    message: 'Bad request',
  },
};

const ACCEPTED = { data: {}, error: null };

describe('useResetPassword: mapping the resolved { error } of resetPasswordForEmail', () => {
  it('maps a swallowed AuthRetryableFetchError to network instead of falsely reporting sent', async () => {
    resetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports too_many_attempts for a rate-limited { error } instead of a false sent', async () => {
    // #657: resetPasswordForEmail resolving with any { error } means the email
    // was never sent, so reporting `sent` is a false success. Supabase succeeds
    // for unknown addresses, so surfacing this error leaks no enumeration signal.
    resetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 429, code: 'over_email_send_rate_limit', message: 'rate limited' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('maps a genuine non-transport { error } to an unknown error state', async () => {
    resetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports sent on success', async () => {
    resetPasswordForEmail.mockResolvedValue({ data: {}, error: null });

    expect(await requestReset()).toEqual({ kind: 'sent' });
  });
});

describe('useResetPassword: refusing a run of failures against one address (#1640)', () => {
  it('stops sending requests to Supabase once the address is locked out', async () => {
    resetPasswordForEmail.mockResolvedValue(REJECTED);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES; i += 1) await requestReset();

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
    expect(resetPasswordForEmail).toHaveBeenCalledTimes(LOCKOUT_AFTER_FAILURES);
  });

  // The screen types the address raw — the hook is what trims it — so a lockout
  // keyed on the untouched text would be shed by adding a space or a capital.
  it('counts the same address in another case as one run', async () => {
    resetPasswordForEmail.mockResolvedValue(REJECTED);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES; i += 1) await requestReset();

    expect(await requestResetFor(' A@B.co ')).toEqual({
      kind: 'error',
      reason: 'too_many_attempts',
    });
  });

  it('forgets the run once a reset email goes out', async () => {
    resetPasswordForEmail.mockResolvedValue(REJECTED);
    for (let i = 0; i < LOCKOUT_AFTER_FAILURES - 1; i += 1) await requestReset();
    resetPasswordForEmail.mockResolvedValue(ACCEPTED);
    await requestReset();

    resetPasswordForEmail.mockResolvedValue(REJECTED);
    expect(await requestReset()).toEqual({ kind: 'error', reason: 'unknown' });
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
      'reset',
      resetPasswordForEmail,
      () => runAsyncAuthHook(useResetPassword, (hook) => hook.requestReset('a@b.co')),
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

describe('useResetPassword: the recovery redirect on web (#2837)', () => {
  afterEach(() => {
    Platform.OS = 'ios';
    Reflect.deleteProperty(globalThis, 'window');
  });

  it('sends the recovery link back to this origin instead of the altune scheme', async () => {
    Platform.OS = 'web';
    Object.assign(globalThis, { window: { location: { origin: 'https://app.altune.example' } } });
    resetPasswordForEmail.mockResolvedValue(ACCEPTED);

    await requestReset();

    expect(resetPasswordForEmail).toHaveBeenCalledWith('a@b.co', {
      redirectTo: 'https://app.altune.example/auth/recovery',
    });
  });
});
