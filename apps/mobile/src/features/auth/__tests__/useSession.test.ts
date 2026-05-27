/**
 * useSession — verifies the discriminated-union transitions from `loading`
 * to `signed-in` / `signed-out` based on the SDK's getSession + auth-state
 * stream.
 */
/* eslint-disable @typescript-eslint/no-require-imports */
import { renderHook, waitFor } from '@testing-library/react-native';

type AuthChangeCallback = (event: string, session: unknown) => void;

const mockGetSession = jest.fn(async () => ({ data: { session: null } }));
const mockUnsubscribe = jest.fn();
let lastAuthChangeCallback: AuthChangeCallback | null = null;
const mockOnAuthStateChange = jest.fn((cb: AuthChangeCallback) => {
  lastAuthChangeCallback = cb;
  return { data: { subscription: { unsubscribe: mockUnsubscribe } } };
});

jest.mock('../api/supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: () => mockGetSession(),
      onAuthStateChange: (cb: AuthChangeCallback) => mockOnAuthStateChange(cb),
    },
  },
}));

beforeEach(() => {
  mockGetSession.mockReset().mockResolvedValue({ data: { session: null } });
  mockOnAuthStateChange.mockClear();
  mockUnsubscribe.mockClear();
  lastAuthChangeCallback = null;
});

describe('useSession', () => {
  it('starts in loading status', () => {
    const { useSession } = require('../hooks/useSession');
    const { result } = renderHook(() => useSession());
    expect(result.current.status).toBe('loading');
  });

  it('transitions to signed-out when getSession returns no session', async () => {
    const { useSession } = require('../hooks/useSession');
    const { result } = renderHook(() => useSession());
    await waitFor(() => expect(result.current.status).toBe('signed-out'));
  });

  it('transitions to signed-in when getSession returns a session', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../hooks/useSession');
    const { result } = renderHook(() => useSession());
    await waitFor(() => expect(result.current.status).toBe('signed-in'));
    if (result.current.status === 'signed-in') {
      expect(result.current.session).toEqual(fakeSession);
    }
  });

  it('transitions to signed-out on a SIGNED_OUT auth event', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../hooks/useSession');
    const { result } = renderHook(() => useSession());
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    // Simulate the SDK emitting SIGNED_OUT.
    expect(lastAuthChangeCallback).not.toBeNull();
    lastAuthChangeCallback!('SIGNED_OUT', null);

    await waitFor(() => expect(result.current.status).toBe('signed-out'));
  });
});
