import type { Session } from '@supabase/supabase-js';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ContractError, LONG_REQUEST_TIMEOUT_MS, NetworkError } from '@shared/api-client';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { detailKeys, libraryKeys } from '@shared/query/queryKeys';
import { RETRY_BACKOFF_BASE_MS, RETRY_BACKOFF_CAP_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '@testing/makeWrapper';
import { useBackfillFeatured } from '@features/settings/hooks/useBackfillFeatured';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

const BACKFILL = 'POST /v1/tracks/featured-backfill';
const ATTEMPT_AND_ONE_RETRY = 2;
const BAD_GATEWAY = {
  status: 502,
  json: { error: { code: 'bad_gateway', message: 'bad gateway' } },
};

type WireRequest = { method: string; path: string; query: string };

function page(over: object) {
  return { scanned: 0, updated: 0, truncated: false, next_offset: 0, ...over };
}

function backfillQueries(): string[] {
  return __http.requests
    .filter((r: WireRequest) => `${r.method} ${r.path}` === BACKFILL)
    .map((r: WireRequest) => r.query);
}

function signedIn() {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  } as never);
}

function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function holdResponses(when: (url: string) => boolean) {
  const gate = deferred();
  global.fetch = ((url: string, init: RequestInit) => {
    const response = fakeFetch(url, init);
    return when(String(url)) ? gate.promise.then(() => response) : response;
  }) as typeof fetch;
  return gate.resolve;
}

afterEach(() => {
  global.fetch = fakeFetch;
  jest.restoreAllMocks();
});

describe('settings mutations retry transient failures', () => {
  beforeEach(() => {
    jest.useFakeTimers();
    signedIn();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('retries a 502 on backfill and settles as success', async () => {
    __http.replyOnce(BACKFILL, BAD_GATEWAY);
    __http.replyOnce(BACKFILL, { json: page({ scanned: 2, updated: 1 }) });

    const hook = renderHook(useBackfillFeatured, { wrapper: makeWrapper(new QueryClient()) });
    act(() => {
      hook.result.current.mutate();
    });
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_BASE_MS);
    });

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(hook.result.current.data).toMatchObject({ scanned: 2, updated: 1 });
    expect(__http.countFor(BACKFILL)).toBe(ATTEMPT_AND_ONE_RETRY);
    hook.unmount();
  });
});

describe('the jittered retry backoff', () => {
  const jitterSamples = [
    { sample: 0, dueMs: RETRY_BACKOFF_BASE_MS / 2 },
    { sample: 0.5, dueMs: (RETRY_BACKOFF_BASE_MS * 3) / 4 },
  ];

  async function elapse(ms: number) {
    await act(async () => {
      await jest.advanceTimersByTimeAsync(ms);
    });
  }

  beforeEach(() => {
    jest.useFakeTimers();
    signedIn();
    __http.reply(BACKFILL, BAD_GATEWAY);
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  describe('backfill spreads its retry over the jittered backoff', () => {
    it.each(jitterSamples)(
      'reattempts at $dueMs ms when the jitter sample is $sample',
      async ({ sample, dueMs }) => {
        jest.spyOn(Math, 'random').mockReturnValue(sample);
        const queryClient = new QueryClient();
        const hook = renderHook(useBackfillFeatured, { wrapper: makeWrapper(queryClient) });
        act(() => {
          hook.result.current.mutate();
        });

        await elapse(dueMs - 1);
        expect(__http.countFor(BACKFILL)).toBe(1);

        await elapse(1);
        expect(__http.countFor(BACKFILL)).toBe(ATTEMPT_AND_ONE_RETRY);

        hook.unmount();
        queryClient.clear();
      },
    );
  });
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
  });

  it("a backfill A started does not invalidate B's library cache when it resolves after the switch", async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    __http.replyOnce(BACKFILL, { json: page({ scanned: 10, updated: 3, next_offset: 10 }) });
    const release = holdResponses(() => true);

    const backfill = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      backfill.result.current.mutate();
    });
    await waitFor(() => expect(__http.countFor(BACKFILL)).toBe(1));
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
      release();
      await new Promise((r) => setTimeout(r, 0));
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

describe('backfill over a library larger than one server call', () => {
  beforeEach(() => {
    signedIn();
  });

  it('follows next_offset until the server stops truncating and sums the counts', async () => {
    __http.replyOnce(BACKFILL, {
      json: page({ scanned: 10000, updated: 3, truncated: true, next_offset: 10000 }),
    });
    __http.replyOnce(BACKFILL, { json: page({ scanned: 40, updated: 1 }) });
    const releaseSecond = holdResponses((url) => url.includes('offset='));

    const hook = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    act(() => {
      hook.result.current.mutate();
    });

    await waitFor(() => expect(backfillQueries()).toEqual(['', 'offset=10000']));
    expect(hook.result.current.status).toBe('pending');

    await act(async () => {
      releaseSecond();
    });
    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(hook.result.current.data).toMatchObject({ scanned: 10040, updated: 4 });
    hook.unmount();
  });

  it('fails instead of looping when next_offset does not advance', async () => {
    __http.reply(BACKFILL, { json: page({ scanned: 5, truncated: true, next_offset: 0 }) });

    const hook = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    act(() => {
      hook.result.current.mutate();
    });

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(hook.result.current.error).toBeInstanceOf(ContractError);
    expect(__http.countFor(BACKFILL)).toBe(1);
    hook.unmount();
  });

  it('does not re-POST after a client timeout', async () => {
    jest.useFakeTimers();
    __http.hang(BACKFILL);

    const hook = renderHook(() => useBackfillFeatured(), {
      wrapper: makeWrapper(new QueryClient()),
    });
    act(() => {
      hook.result.current.mutate();
    });
    await act(async () => {
      await jest.advanceTimersByTimeAsync(LONG_REQUEST_TIMEOUT_MS + RETRY_BACKOFF_CAP_MS);
    });

    expect(hook.result.current.isError).toBe(true);
    expect(hook.result.current.error).toBeInstanceOf(NetworkError);
    expect((hook.result.current.error as NetworkError).failure).toBe('timeout');
    expect(__http.countFor(BACKFILL)).toBe(1);
    hook.unmount();
    jest.useRealTimers();
  });
});
