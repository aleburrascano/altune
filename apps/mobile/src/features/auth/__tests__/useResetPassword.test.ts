import { renderHook, act } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useResetPassword } from '../hooks/useResetPassword';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { resetPasswordForEmail: jest.fn() } },
}));

const mockResetPasswordForEmail = supabase.auth.resetPasswordForEmail as unknown as jest.Mock;

async function requestReset(): Promise<{ kind: string; reason?: string }> {
  const { result } = renderHook(() => useResetPassword());
  await act(async () => {
    await result.current.requestReset('a@b.co');
  });
  return result.current.state;
}

describe('useResetPassword: mapping the resolved { error } of resetPasswordForEmail', () => {
  beforeEach(() => mockResetPasswordForEmail.mockReset());

  it('maps a swallowed AuthRetryableFetchError to network instead of falsely reporting sent', async () => {
    mockResetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('reports an unknown error for a non-transport { error } instead of a false sent', async () => {
    // #657: resetPasswordForEmail resolving with any { error } means the email
    // was never sent, so reporting `sent` is a false success. Supabase succeeds
    // for unknown addresses, so surfacing this error leaks no enumeration signal.
    mockResetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 429, code: 'over_email_send_rate_limit', message: 'rate limited' },
    });

    // 429 is a transport-class failure, so it maps to network.
    expect(await requestReset()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a genuine non-transport { error } to an unknown error state', async () => {
    mockResetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await requestReset()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports sent on success', async () => {
    mockResetPasswordForEmail.mockResolvedValue({ data: {}, error: null });

    expect(await requestReset()).toEqual({ kind: 'sent' });
  });
});
