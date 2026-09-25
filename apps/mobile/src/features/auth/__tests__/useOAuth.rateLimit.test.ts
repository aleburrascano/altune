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

describe('useOAuth: a server rate limit is not a network error', () => {
  beforeEach(() => {
    signInWithOAuth.mockResolvedValue({
      data: { url: 'https://accounts.google.com/o' },
      error: null,
    });
    openAuthSessionAsync.mockReset().mockResolvedValue({
      type: 'success',
      url: 'altune://auth/callback?code=abc',
    });
    mockComplete.mockReset();
  });

  it('maps a 429 on the authorization request to too_many_attempts', async () => {
    signInWithOAuth.mockResolvedValue({
      data: null,
      error: { status: 429, code: 'over_request_rate_limit' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('maps a 429 on the code exchange to too_many_attempts', async () => {
    mockComplete.mockResolvedValue({
      kind: 'failure',
      error: { status: 429, code: 'over_request_rate_limit' },
    });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'too_many_attempts' });
  });

  it('still maps a 503 on the authorization request to network', async () => {
    signInWithOAuth.mockResolvedValue({ data: null, error: { status: 503 } });

    expect(await signIn()).toEqual({ kind: 'error', reason: 'network' });
  });
});
