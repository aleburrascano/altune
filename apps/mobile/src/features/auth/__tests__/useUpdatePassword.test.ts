import { renderHook, act } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useUpdatePassword } from '../hooks/useUpdatePassword';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { updateUser: jest.fn() } },
}));

const mockUpdateUser = supabase.auth.updateUser as unknown as jest.Mock;

async function updatePassword(): Promise<{ kind: string; reason?: string }> {
  const { result } = renderHook(() => useUpdatePassword());
  await act(async () => {
    await result.current.updatePassword('new-password');
  });
  return result.current.state;
}

describe('useUpdatePassword: mapping the resolved { error } of updateUser', () => {
  beforeEach(() => mockUpdateUser.mockReset());

  it('maps a swallowed AuthRetryableFetchError to network, not unknown', async () => {
    mockUpdateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps an AuthWeakPasswordError to weak_password', async () => {
    mockUpdateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthWeakPasswordError', status: 422, code: 'weak_password', message: 'Password is too weak' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'weak_password' });
  });

  it('falls back to unknown for an unrecognised error', async () => {
    mockUpdateUser.mockResolvedValue({
      data: { user: null },
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await updatePassword()).toEqual({ kind: 'error', reason: 'unknown' });
  });

  it('reports ok on success', async () => {
    mockUpdateUser.mockResolvedValue({ data: { user: {} }, error: null });

    expect(await updatePassword()).toEqual({ kind: 'ok' });
  });
});
