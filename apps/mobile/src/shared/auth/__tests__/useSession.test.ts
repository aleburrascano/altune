import { renderHook, waitFor } from '@testing-library/react-native';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import type { ReactNode } from 'react';
import { createElement } from 'react';

type AuthChangeCallback = (event: string, session: unknown) => void;

const mockGetSession = jest.fn(async () => ({ data: { session: null } }));
const mockUnsubscribe = jest.fn();
let lastAuthChangeCallback: AuthChangeCallback | null = null;
const mockOnAuthStateChange = jest.fn((cb: AuthChangeCallback) => {
  lastAuthChangeCallback = cb;
  return { data: { subscription: { unsubscribe: mockUnsubscribe } } };
});

jest.mock('../supabaseClient', () => ({
  supabase: {
    auth: {
      getSession: () => mockGetSession(),
      onAuthStateChange: (cb: AuthChangeCallback) => mockOnAuthStateChange(cb),
    },
  },
}));

let qc: QueryClient;
function _wrapper({ children }: { children: ReactNode }) {
  return createElement(QueryClientProvider, { client: qc }, children);
}

beforeEach(() => {
  mockGetSession.mockReset().mockResolvedValue({ data: { session: null } });
  mockOnAuthStateChange.mockClear();
  mockUnsubscribe.mockClear();
  lastAuthChangeCallback = null;
  qc = new QueryClient();
});

describe('useSession', () => {
  it('starts in loading status', () => {
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    expect(result.current.status).toBe('loading');
  });

  it('transitions to signed-out when getSession returns no session', async () => {
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-out'));
  });

  it('transitions to signed-in when getSession returns a session', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));
    if (result.current.status === 'signed-in') {
      expect(result.current.session).toEqual(fakeSession);
    }
  });

  it('transitions to signed-out on a SIGNED_OUT auth event', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    expect(lastAuthChangeCallback).not.toBeNull();
    lastAuthChangeCallback!('SIGNED_OUT', null);

    await waitFor(() => expect(result.current.status).toBe('signed-out'));
  });

  it('clears the query cache on an SDK-initiated sign-out', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    qc.setQueryData(['library-home'], { items: [{ id: 'user-a-track' }] });
    lastAuthChangeCallback!('SIGNED_OUT', null);

    await waitFor(() => expect(qc.getQueryData(['library-home'])).toBeUndefined());
  });

  it('clears the query cache when the session switches to a different user', async () => {
    const sessionA = { access_token: 'a', user: { id: 'u1' } };
    const sessionB = { access_token: 'b', user: { id: 'u2' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: sessionA } } as never);
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    qc.setQueryData(['library-home'], { items: [{ id: 'user-a-track' }] });
    lastAuthChangeCallback!('SIGNED_IN', sessionB);

    await waitFor(() => expect(qc.getQueryData(['library-home'])).toBeUndefined());
  });

  it('does NOT clear the cache on TOKEN_REFRESHED for the same user', async () => {
    const fakeSession = { access_token: 'abc', user: { id: 'u1' } };
    mockGetSession.mockResolvedValueOnce({ data: { session: fakeSession } } as never);
    const { useSession } = require('../useSession');
    const { result } = renderHook(() => useSession(), { wrapper: _wrapper });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    qc.setQueryData(['library-home'], { items: [{ id: 'kept' }] });
    lastAuthChangeCallback!('TOKEN_REFRESHED', { access_token: 'rotated', user: { id: 'u1' } });

    await waitFor(() => expect(result.current.status).toBe('signed-in'));
    expect(qc.getQueryData(['library-home'])).toEqual({ items: [{ id: 'kept' }] });
  });
});
