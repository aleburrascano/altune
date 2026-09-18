import { renderHook, act } from '@testing-library/react-native';
import * as WebBrowser from 'expo-web-browser';

import { completeAuthIntent } from '../completeAuthIntent';
import { useOAuth } from '../hooks/useOAuth';

import { createSupabaseAuthMock } from './testUtils/authTestUtils';

jest.mock('expo-router', () => ({ useRouter: () => ({ replace: jest.fn() }) }));
jest.mock('expo-web-browser', () => ({
  maybeCompleteAuthSession: jest.fn(),
  openAuthSessionAsync: jest.fn(),
}));
jest.mock('@shared/auth/supabaseClient', () => ({ supabase: { auth: {} } }));
jest.mock('../completeAuthIntent', () => ({ completeAuthIntent: jest.fn() }));

const { signInWithOAuth } = createSupabaseAuthMock('signInWithOAuth');
const openAuthSessionAsync = WebBrowser.openAuthSessionAsync as unknown as jest.Mock;
const mockComplete = completeAuthIntent as jest.Mock;

async function signIn(): Promise<{ kind: string }> {
  const { result } = renderHook(() => useOAuth());
  await act(async () => {
    await result.current.signInWith('google');
  });
  return result.current.state;
}

describe('useOAuth: deriving the terminal state from the real exchange outcome (#657)', () => {
  beforeEach(() => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    openAuthSessionAsync
      .mockReset()
      .mockResolvedValue({ type: 'success', url: 'altune://auth/callback?code=abc' });
    mockComplete.mockReset().mockResolvedValue({ kind: 'success' });
  });

  it('reports ok when completeAuthIntent confirms a successful exchange', async () => {
    mockComplete.mockResolvedValue({ kind: 'success' });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });

  it('reports error instead of a false ok when the exchange fails', async () => {
    mockComplete.mockResolvedValue({ kind: 'failure' });

    expect(await signIn()).toEqual({ kind: 'error' });
  });

  it('treats a deduped callback as ok because the deep-link listener established the session', async () => {
    mockComplete.mockResolvedValue({ kind: 'deduped' });

    expect(await signIn()).toEqual({ kind: 'ok' });
  });

  it('reports error when signInWithOAuth returns { error } before the browser opens', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { message: 'nope' } });

    expect(await signIn()).toEqual({ kind: 'error' });
    expect(openAuthSessionAsync).not.toHaveBeenCalled();
  });

  it('reports cancelled when the user dismisses the auth browser', async () => {
    openAuthSessionAsync.mockResolvedValue({ type: 'cancel' });

    expect(await signIn()).toEqual({ kind: 'cancelled' });
    expect(mockComplete).not.toHaveBeenCalled();
  });
});
