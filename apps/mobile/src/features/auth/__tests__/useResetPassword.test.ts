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
