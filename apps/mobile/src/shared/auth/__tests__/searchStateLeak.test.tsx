import React from 'react';
import type { Session } from '@supabase/supabase-js';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act, waitFor } from '@testing-library/react-native';

import { getSearchState, setSearchState } from '@features/discover/search-state';
import type { DiscoveryResult } from '@shared/api-client/discovery';
import {
  clearDetailHandoff,
  getDetailHandoff,
  getDetailHandoffSearchId,
  setDetailHandoff,
} from '@shared/lib/detail-handoff';

import { useSession } from '../useSession';
import { useSignOut } from '../useSignOut';
import { supabase } from '../supabaseClient';

type AuthCallback = (event: string, session: Session | null) => void;

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

function sessionFor(user: Session['user']): Session {
  return {
    access_token: `token-${user.id}`,
    refresh_token: `refresh-${user.id}`,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expires_in: 3600,
    token_type: 'bearer',
    user,
  } as Session;
}

const TAPPED: DiscoveryResult = {
  kind: 'track',
  title: 'Anti-Hero',
  subtitle: 'Taylor Swift',
  image_url: null,
  confidence: 'high',
  sources: [],
  extras: {},
};

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

let authCallbacks: AuthCallback[] = [];

function bootSignedInAs(user: Session['user']): void {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: sessionFor(user) }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
}

function emitAuth(event: string, session: Session | null): void {
  act(() => {
    authCallbacks.forEach((cb) => cb(event, session));
  });
}

function userASearchedAndTapped(): void {
  setSearchState('taylor swift', 'taylor swift');
  setDetailHandoff(TAPPED, 'search-of-user-a');
}

function expectNothingOfUserALeft(): void {
  expect(getSearchState()).toEqual({ query: '', inputValue: '' });
  expect(getDetailHandoff()).toBeNull();
  expect(getDetailHandoffSearchId()).toBeNull();
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
  setSearchState('', '');
  clearDetailHandoff();
});

describe('search text and last-tapped result do not survive an identity change (#772)', () => {
  it.each([
    ['user A signs out', 'SIGNED_OUT', null],
    ['the device switches straight to user B', 'SIGNED_IN', sessionFor(USER_B)],
  ] as const)('%s -> search-state and detail-handoff are cleared', async (_label, event, next) => {
    bootSignedInAs(USER_A);
    const { result } = renderHook(() => useSession(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    userASearchedAndTapped();
    emitAuth(event, next);

    expectNothingOfUserALeft();
  });

  it('a token refresh for the same user keeps the search the user is in the middle of', async () => {
    bootSignedInAs(USER_A);
    const { result } = renderHook(() => useSession(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    await waitFor(() => expect(result.current.status).toBe('signed-in'));

    userASearchedAndTapped();
    emitAuth('TOKEN_REFRESHED', sessionFor(USER_A));

    expect(getSearchState()).toEqual({ query: 'taylor swift', inputValue: 'taylor swift' });
    expect(getDetailHandoff()).toEqual(TAPPED);
  });

  it.each([
    ['succeeds', () => ({ error: null })],
    ['returns an error', () => ({ error: { message: 'network', status: 0 } })],
    [
      'throws',
      () => {
        throw new Error('network request failed');
      },
    ],
  ] as const)(
    'useSignOut clears both even when supabase signOut %s and no auth event arrives',
    async (_label, outcome) => {
      jest.spyOn(supabase.auth, 'signOut').mockImplementation((async () => outcome()) as never);
      const { result } = renderHook(() => useSignOut(), {
        wrapper: makeWrapper(new QueryClient()),
      });

      userASearchedAndTapped();
      await act(async () => {
        await result.current.signOut();
      });

      expectNothingOfUserALeft();
    },
  );
});
