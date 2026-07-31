import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, act } from '@testing-library/react-native';
import fc from 'fast-check';
import type { AuthChangeEvent, Session } from '@supabase/supabase-js';

import { usePinnedStore } from '@shared/offline/pinnedStore';

import { useSession } from '../useSession';
import { supabase } from '../supabaseClient';

jest.mock('../supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn(), onAuthStateChange: jest.fn() } },
}));

type Listener = (event: AuthChangeEvent, session: Session | null) => void;

function installAuth() {
  const getSession = supabase.auth.getSession as jest.Mock;
  const onAuthStateChange = supabase.auth.onAuthStateChange as jest.Mock;
  let listener: Listener | null = null;

  onAuthStateChange.mockImplementation((cb: Listener) => {
    listener = cb;
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  });
  getSession.mockReturnValue(new Promise<never>(() => {}));

  return {
    emit(event: AuthChangeEvent, session: Session | null): void {
      if (!listener) throw new Error('onAuthStateChange was never subscribed to');
      listener(event, session);
    },
  };
}

function makeSession(userId: string): Session {
  return {
    access_token: `token-${userId}`,
    refresh_token: `refresh-${userId}`,
    expires_in: 3600,
    token_type: 'bearer',
    user: { id: userId } as unknown as Session['user'],
  } as Session;
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

const SENTINEL_KEY = ['sentinel'];
const idArb = fc.constantFrom('a', 'b', 'c');
const emissionArb = fc.option(idArb, { nil: null });

function driveSequenceAndAssertLaw(sequence: (string | null)[]): void {
  const auth = installAuth();
  const queryClient = new QueryClient();
  const { unmount } = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });

  let previous: string | null | undefined;
  for (const emission of sequence) {
    queryClient.setQueryData(SENTINEL_KEY, 'present');

    act(() => {
      auth.emit('SIGNED_IN', emission === null ? null : makeSession(emission));
    });

    const wiped = queryClient.getQueryData(SENTINEL_KEY) === undefined;
    const expectedWipe = previous !== undefined && previous !== emission;
    expect(wiped).toBe(expectedWipe);
    previous = emission;
  }

  unmount();
}

beforeEach(() => {
  jest.clearAllMocks();
  usePinnedStore.setState({ entries: {}, queue: [], isWorking: false });
});

describe('law: the local-data wipe runs exactly once per adjacent distinct pair, never on the first, never on a repeat', () => {
  it('holds for a random sequence of session emissions, checked after every step', () => {
    fc.assert(
      fc.property(fc.array(emissionArb, { maxLength: 10 }), (sequence) => {
        driveSequenceAndAssertLaw(sequence);
      }),
      { numRuns: 100 },
    );
  });

  it.each<[string, (string | null)[]]>([
    ['empty sequence', []],
    ['single signed-in observation never wipes', ['a']],
    ['single signed-out observation never wipes', [null]],
    ['immediate repeat never wipes', ['a', 'a']],
    ['immediate distinct pair wipes once', ['a', 'b']],
    ['alternating users wipes on every step after the first', ['a', 'b', 'a', 'b']],
    ['sign-in, sign-out, sign back in as someone else', ['a', null, 'b']],
    ['three repeats in a row wipes zero times', ['a', 'a', 'a']],
  ])('worked example: %s', (_label, sequence) => {
    driveSequenceAndAssertLaw(sequence);
  });
});
