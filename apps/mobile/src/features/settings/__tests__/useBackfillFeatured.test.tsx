import type { Session } from '@supabase/supabase-js';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { backfillFeaturedArtists } from '@shared/api-client/tracks';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { detailKeys, libraryKeys } from '@shared/lib/query-keys';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '../../../../jest/makeWrapper';
import { useBackfillFeatured } from '../hooks/useBackfillFeatured';

jest.mock('@shared/api-client/tracks', () => ({
  ...jest.requireActual('@shared/api-client/tracks'),
  backfillFeaturedArtists: jest.fn(),
}));

describe('settings mutations retry transient failures', () => {
  // #841: mutations default to zero retries, so a transient 502 on backfill or
  // clear-history failed outright. They must retry transient failures via isRetryable().

  function makeClient() {
    // No mutation defaults: each hook owns both the retry decision and its delay.
    return new QueryClient();
  }

  // The hooks' own jittered backoff (#1756) puts the reattempt somewhere below this
  // ceiling rather than at once, so the clock has to run before a retry can land.
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
    jest.mocked(backfillFeaturedArtists).mockReset();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('retries a 502 on backfill and settles as success', async () => {
    jest
      .mocked(backfillFeaturedArtists)
      .mockRejectedValueOnce(badGateway())
      .mockResolvedValueOnce({ updated: 0 } as never);

    const hook = renderMutation(useBackfillFeatured);
    await elapsePastTheFirstRetry();

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(backfillFeaturedArtists).toHaveBeenCalledTimes(2);
    hook.unmount();
  });
});

describe('the jittered retry backoff', () => {
  // #1756: these mutations set no retryDelay, so they inherited react-query's fixed
  // 1000ms first backoff and every client failing on one outage retried together.

  const retryingMutations = [
    {
      name: 'backfill',
      useMutationHook: useBackfillFeatured,
      api: () => backfillFeaturedArtists as jest.Mock,
    },
  ];

  // Equal jitter puts the first retry at half the base ceiling plus the sample's
  // share of the other half; un-jittered every sample would land on the ceiling.
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
    (backfillFeaturedArtists as jest.Mock)
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
  // #836: a settings mutation A starts, then A signs out and B signs in before it
  // settles. The late response must not invalidate or repopulate B's query cache.

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
    // Earlier tests in this file leave calls on the module mocks.
    jest.mocked(backfillFeaturedArtists).mockReset();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

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
});
