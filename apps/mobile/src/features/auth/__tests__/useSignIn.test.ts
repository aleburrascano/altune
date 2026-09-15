import { renderHook, act } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useSignIn } from '../hooks/useSignIn';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signInWithPassword: jest.fn() } },
}));

const mockSignInWithPassword = supabase.auth.signInWithPassword as unknown as jest.Mock;

async function signIn(): Promise<{ kind: string; reason?: string }> {
  const { result } = renderHook(() => useSignIn());
  await act(async () => {
    await result.current.signIn('a@b.co', 'pw');
  });
  return result.current.state;
}

describe('useSignIn: mapping the resolved { error } of signInWithPassword', () => {
  beforeEach(() => mockSignInWithPassword.mockReset());

  it('maps a swallowed AuthRetryableFetchError to network, never invalid_credentials', async () => {
    mockSignInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a 503 reachability failure to network', async () => {
    mockSignInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 503, message: 'Service Unavailable' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps a genuine 400 invalid_credentials to invalid_credentials', async () => {
    mockSignInWithPassword.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'invalid_credentials', message: 'Invalid login credentials' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'invalid_credentials' });
  });

  it('reports ok when Supabase returns a session', async () => {
    mockSignInWithPassword.mockResolvedValue({ data: { user: {}, session: {} }, error: null });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });
});
