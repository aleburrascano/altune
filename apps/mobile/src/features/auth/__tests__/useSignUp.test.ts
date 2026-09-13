import { renderHook, act } from '@testing-library/react-native';

import { supabase } from '@shared/auth/supabaseClient';

import { useSignUp } from '../hooks/useSignUp';

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { signUp: jest.fn() } },
}));

const mockSignUp = supabase.auth.signUp as unknown as jest.Mock;

async function signUp(): Promise<{ kind: string; reason?: string }> {
  const { result } = renderHook(() => useSignUp());
  await act(async () => {
    await result.current.signUp('a@b.co', 'pw');
  });
  return result.current.state;
}

describe('useSignUp: mapping the resolved outcome of signUp', () => {
  beforeEach(() => mockSignUp.mockReset());

  it('maps a swallowed AuthRetryableFetchError to network', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthRetryableFetchError', status: 0, message: 'Failed to fetch' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'network' });
  });

  it('maps an AuthWeakPasswordError to weak_password', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthWeakPasswordError', status: 422, code: 'weak_password', message: 'Password is too weak' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'weak_password' });
  });

  it('maps an explicit user_already_exists error to already_registered', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 422, code: 'user_already_exists', message: 'User already registered' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'already_registered' });
  });

  it('maps the empty-identities anti-enumeration signal to already_registered, not awaiting-confirmation', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: { id: 'obfuscated', identities: [] }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'already_registered' });
  });

  it('reports awaiting-confirmation for a fresh signup with identities but no session', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: { id: 'new', identities: [{ id: 'i1' }] }, session: null },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'awaiting-confirmation' });
  });

  it('reports ok when a session is returned immediately', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: { id: 'new', identities: [{ id: 'i1' }] }, session: {} },
      error: null,
    });

    expect(await signUp()).toEqual({ kind: 'ok' });
  });

  it('falls back to unknown for an unrecognised error code', async () => {
    mockSignUp.mockResolvedValue({
      data: { user: null, session: null },
      error: { name: 'AuthApiError', status: 400, code: 'validation_failed', message: 'Bad request' },
    });

    expect(await signUp()).toEqual({ kind: 'error', reason: 'unknown' });
  });
});
