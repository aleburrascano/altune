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

  it('still reports sent for a non-transport error, preserving anti-enumeration', async () => {
    mockResetPasswordForEmail.mockResolvedValue({
      data: null,
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await requestReset()).toEqual({ kind: 'sent' });
  });

  it('reports sent on success', async () => {
    mockResetPasswordForEmail.mockResolvedValue({ data: {}, error: null });

    expect(await requestReset()).toEqual({ kind: 'sent' });
  });
});
