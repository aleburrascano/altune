import type { Session } from '@supabase/supabase-js';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError, authorization } from '@shared/api-client';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { detailKeys, discoveryKeys, libraryKeys } from '@shared/lib/query-keys';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';

// #836: a settings mutation A starts, then A signs out and B signs in before it
// settles. The late response must not invalidate or repopulate B's query cache.

jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));
jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));

type AuthCallback = (event: string, session: Session | null) => void;

const USER_A = { id: 'aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa' } as Session['user'];
const USER_B = { id: 'bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb' } as Session['user'];

function sessionFor(user: Session['user'], token: string): Session {
  return {
    access_token: token,
    refresh_token: `${token}-refresh`,
    expires_at: Math.floor(Date.now() / 1000) + 3600,
    expires_in: 3600,
    token_type: 'bearer',
    user,
  } as Session;
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let authCallbacks: AuthCallback[] = [];

async function bootAsUserA(queryClient: QueryClient) {
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: sessionFor(USER_A, 'token-a') }, error: null } as never);
  jest.spyOn(supabase.auth, 'onAuthStateChange').mockImplementation(((cb: AuthCallback) => {
    authCallbacks.push(cb);
    return { data: { subscription: { unsubscribe: jest.fn() } } };
  }) as never);
  jest.spyOn(supabase.auth, 'signOut').mockImplementation((async () => {
    authCallbacks.forEach((cb) => cb('SIGNED_OUT', null));
    return { error: null };
  }) as never);
  const session = renderHook(() => useSession(), { wrapper: makeWrapper(queryClient) });
  await waitFor(() => expect(session.result.current.status).toBe('signed-in'));
  return session;
}

async function signOutThenSignInAsUserB(queryClient: QueryClient): Promise<void> {
  const signOut = renderHook(() => useSignOut(), { wrapper: makeWrapper(queryClient) });
  await act(async () => {
    await signOut.result.current.signOut();
  });
  signOut.unmount();
  const sessionOfB = sessionFor(USER_B, 'token-b');
  // B's is now the session `authorization()` reads, as it would be on the device.
  jest
    .spyOn(supabase.auth, 'getSession')
    .mockResolvedValue({ data: { session: sessionOfB }, error: null } as never);
  act(() => {
    authCallbacks.forEach((cb) => cb('SIGNED_IN', sessionOfB));
  });
}

function isInvalidated(queryClient: QueryClient, key: readonly unknown[]): boolean | undefined {
  return queryClient.getQueryState(key)?.isInvalidated;
}

beforeEach(() => {
  authCallbacks = [];
  jest.restoreAllMocks();
});

afterEach(() => {
  jest.useRealTimers();
});

describe('settings mutations racing a sign-out (#836)', () => {
  it("a backfill A started does not invalidate B's library cache when it resolves after the switch", async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    const pending = deferred<{ scanned: number; updated: number }>();
    jest.mocked(backfillFeaturedArtists).mockReturnValue(pending.promise);

    const backfill = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      backfill.result.current.mutate();
    });
    await waitFor(() => expect(backfillFeaturedArtists).toHaveBeenCalledTimes(1));
    // Settings unmounts with the signed-in tree, as it does in the app.
    backfill.unmount();

    await signOutThenSignInAsUserB(queryClient);
    const tracksKey = [...libraryKeys.tracksPrefix, 'b'];
    const featuringKey = [...libraryKeys.featuringPrefix, 'b'];
    const albumKey = [...detailKeys.albumTracksPrefix, 'b'];
    queryClient.setQueryData(tracksKey, ['track-of-b']);
    queryClient.setQueryData(featuringKey, ['featuring-of-b']);
    queryClient.setQueryData(albumKey, ['album-track-of-b']);
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');

    await act(async () => {
      pending.resolve({ scanned: 10, updated: 3 });
      await pending.promise;
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(invalidate).not.toHaveBeenCalled();
    expect(isInvalidated(queryClient, tracksKey)).toBe(false);
    expect(isInvalidated(queryClient, featuringKey)).toBe(false);
    expect(isInvalidated(queryClient, albumKey)).toBe(false);
    expect(queryClient.getQueryData(tracksKey)).toEqual(['track-of-b']);

    session.unmount();
  });

  it("a failed clear-history A started does not invalidate B's search history", async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    const pending = deferred<void>();
    jest.mocked(clearSearchHistory).mockReturnValue(pending.promise);

    const clear = renderHook(() => useClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      clear.result.current.mutate();
    });
    await waitFor(() => expect(clearSearchHistory).toHaveBeenCalledTimes(1));
    clear.unmount();

    await signOutThenSignInAsUserB(queryClient);
    const historyOfB = { items: [{ query: 'b searched this' }] };
    queryClient.setQueryData(discoveryKeys.history, historyOfB);
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');

    await act(async () => {
      pending.reject(new Error('network request failed'));
      await pending.promise.catch(() => undefined);
    });
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(invalidate).not.toHaveBeenCalled();
    expect(isInvalidated(queryClient, discoveryKeys.history)).toBe(false);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(historyOfB);

    session.unmount();
  });

  // #1752: the settle fence above runs too late for a retry. Every attempt re-derives
  // its bearer token at send time, so a reattempt that fires during the backoff after
  // A left would DELETE B's history on the server before any callback is reached.
  it("does not reattempt A's clear-history once B is the signed-in user", async () => {
    jest.useFakeTimers();
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    const authorizationPerAttempt: string[] = [];
    jest.mocked(clearSearchHistory).mockImplementation(async () => {
      authorizationPerAttempt.push(await authorization('/discovery/search-history', undefined));
      throw new ApiError(502, 'bad gateway');
    });

    const clear = renderHook(() => useClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      clear.result.current.mutate();
    });
    await waitFor(() => expect(clearSearchHistory).toHaveBeenCalledTimes(1));
    clear.unmount();

    await signOutThenSignInAsUserB(queryClient);
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_BASE_MS);
    });

    expect(authorizationPerAttempt).toEqual(['Bearer token-a']);

    session.unmount();
  });

  it('within one session the backfill and clear-history cache effects still apply', async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    jest.mocked(backfillFeaturedArtists).mockResolvedValue({ scanned: 1, updated: 1 });
    jest.mocked(clearSearchHistory).mockRejectedValue(new Error('boom'));
    const tracksKey = [...libraryKeys.tracksPrefix, 'a'];
    queryClient.setQueryData(tracksKey, ['track-of-a']);
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'a' }] });

    const hooks = renderHook(
      () => ({ backfill: useBackfillFeatured(), clear: useClearSearchHistory() }),
      { wrapper: makeWrapper(queryClient) },
    );
    act(() => {
      hooks.result.current.backfill.mutate();
      hooks.result.current.clear.mutate();
    });

    await waitFor(() => expect(isInvalidated(queryClient, tracksKey)).toBe(true));
    await waitFor(() => expect(isInvalidated(queryClient, discoveryKeys.history)).toBe(true));
    // The optimistic clear still ran before the failure.
    expect(hooks.result.current.clear.isError).toBe(true);

    hooks.unmount();
    session.unmount();
  });
});
