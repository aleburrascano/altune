import type { Session } from '@supabase/supabase-js';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError, authorization } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { discoveryKeys, libraryKeys } from '@shared/lib/query-keys';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

describe('useClearSearchHistory racing an in-flight history fetch', () => {
  it('keeps the cleared list when a pre-clear fetch resolves after the optimistic clear', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const beforeClear = { items: [{ query: 'old search' }] };
    queryClient.setQueryData(discoveryKeys.history, beforeClear);
    const staleFetch = deferred<typeof beforeClear>();
    void queryClient
      .fetchQuery({ queryKey: discoveryKeys.history, queryFn: () => staleFetch.promise })
      .catch(() => undefined);
    jest.mocked(clearSearchHistory).mockResolvedValue(undefined);

    const hook = renderHook(() => useClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      hook.result.current.mutate();
    });
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));

    await act(async () => {
      staleFetch.resolve(beforeClear);
      await staleFetch.promise;
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    hook.unmount();
  });
});

describe('settings mutations retry transient failures', () => {
  function makeClient() {
    return new QueryClient();
  }

  const FIRST_RETRY_CEILING_MS = RETRY_BACKOFF_BASE_MS;

  async function elapsePastTheFirstRetry() {
    await act(async () => {
      await jest.advanceTimersByTimeAsync(FIRST_RETRY_CEILING_MS);
    });
  }

  function renderMutation<T extends { mutate: (v?: never) => void }>(useHook: () => T) {
    const hook = renderHook(useHook, { wrapper: makeWrapper(makeClient()) });
    act(() => {
      hook.result.current.mutate();
    });
    return hook;
  }

  const badGateway = () => new ApiError(502, 'bad gateway');

  beforeEach(() => {
    jest.useFakeTimers();
    jest.mocked(clearSearchHistory).mockReset();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('retries a 502 on clear-history and settles as success', async () => {
    jest
      .mocked(clearSearchHistory)
      .mockRejectedValueOnce(badGateway())
      .mockResolvedValueOnce(undefined);

    const hook = renderMutation(useClearSearchHistory);
    await elapsePastTheFirstRetry();

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(clearSearchHistory).toHaveBeenCalledTimes(2);
    hook.unmount();
  });

  it('does not retry a permanent 4xx on clear-history', async () => {
    jest.mocked(clearSearchHistory).mockRejectedValue(new ApiError(400, 'bad request'));

    const hook = renderMutation(useClearSearchHistory);

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(clearSearchHistory).toHaveBeenCalledTimes(1);
    hook.unmount();
  });
});

describe('the jittered retry backoff', () => {
  const retryingMutations = [
    {
      name: 'clear-history',
      useMutationHook: useClearSearchHistory,
      api: () => clearSearchHistory as jest.Mock,
    },
  ];

  const jitterSamples = [
    { sample: 0, dueMs: RETRY_BACKOFF_BASE_MS / 2 },
    { sample: 0.5, dueMs: (RETRY_BACKOFF_BASE_MS * 3) / 4 },
  ];

  function startMutation(useMutationHook: () => { mutate: (v?: never) => void }) {
    const queryClient = new QueryClient();
    const hook = renderHook(useMutationHook, { wrapper: makeWrapper(queryClient) });
    act(() => {
      hook.result.current.mutate();
    });
    return () => {
      hook.unmount();
      queryClient.clear();
    };
  }

  async function elapse(ms: number) {
    await act(async () => {
      await jest.advanceTimersByTimeAsync(ms);
    });
  }

  beforeEach(() => {
    jest.useFakeTimers();
    (clearSearchHistory as jest.Mock)
      .mockReset()
      .mockRejectedValue(new ApiError(502, 'bad gateway'));
  });

  afterEach(() => {
    jest.useRealTimers();
    jest.restoreAllMocks();
  });

  describe.each(retryingMutations)(
    '$name spreads its retry over the jittered backoff',
    ({ useMutationHook, api }) => {
      it.each(jitterSamples)(
        'reattempts at $dueMs ms when the jitter sample is $sample',
        async ({ sample, dueMs }) => {
          jest.spyOn(Math, 'random').mockReturnValue(sample);
          const stop = startMutation(useMutationHook);

          await elapse(dueMs - 1);
          expect(api()).toHaveBeenCalledTimes(1);

          await elapse(1);
          expect(api()).toHaveBeenCalledTimes(2);

          stop();
        },
      );
    },
  );
});

describe('settings mutations racing a sign-out', () => {
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

  let authCallbacks: AuthCallback[] = [];

  async function bootAsUserA(queryClient: QueryClient) {
    jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
      data: { session: sessionFor(USER_A, 'token-a') },
      error: null,
    } as never);
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
    jest.mocked(backfillFeaturedArtists).mockReset();
    jest.mocked(clearSearchHistory).mockReset();
  });

  afterEach(() => {
    jest.useRealTimers();
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
    expect(hooks.result.current.clear.isError).toBe(true);

    hooks.unmount();
    session.unmount();
  });
});
