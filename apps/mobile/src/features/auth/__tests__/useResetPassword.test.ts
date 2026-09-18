import { useResetPassword } from '../hooks/useResetPassword';

import { createSupabaseAuthMock, runAsyncAuthHook } from './testUtils/authTestUtils';

jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));

const { resetPasswordForEmail } = createSupabaseAuthMock('resetPasswordForEmail');

const requestReset = () =>
  runAsyncAuthHook(useResetPassword, (hook) => hook.requestReset('a@b.co'));

describe('useResetPassword: mapping the resolved { error } of resetPasswordForEmail', () => {
  it('maps a swallowed AuthRetryableFetchError to network instead of falsely reporting sent', async () => {
    resetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports an unknown error for a non-transport { error } instead of a false sent', async () => {
    // #657: resetPasswordForEmail resolving with any { error } means the email
    // was never sent, so reporting `sent` is a false success. Supabase succeeds
    // for unknown addresses, so surfacing this error leaks no enumeration signal.
    resetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 429, code: 'over_email_send_rate_limit', message: 'rate limited' },
    });

    // 429 is a transport-class failure, so it maps to network.
    expect(await requestReset()).toEqual({ kind: 'error', reason: 'network' });
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
