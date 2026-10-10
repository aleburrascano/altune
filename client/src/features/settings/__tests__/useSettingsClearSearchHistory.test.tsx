import type { Session } from '@supabase/supabase-js';
import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/apiClient';
import { supabase } from '@shared/auth/supabaseClient';
import { useSession } from '@shared/auth/useSession';
import { useSignOut } from '@shared/auth/useSignOut';
import { discoveryKeys, libraryKeys } from '@shared/query/queryKeys';
import { RETRY_BACKOFF_BASE_MS } from '@shared/query/retryDelay';

import { makeWrapper } from '@testing/makeWrapper';
import { useBackfillFeatured } from '@features/settings/hooks/useBackfillFeatured';
import { useSettingsClearSearchHistory } from '@features/settings/hooks/useSettingsClearSearchHistory';

const { __http, fakeFetch } = require('@testing/doubles/fetch.js');

const CLEAR = 'DELETE /v1/discovery/search-history';
const BACKFILL = 'POST /v1/tracks/featured-backfill';
const CLEARED = { status: 204 };
const ATTEMPT_AND_ONE_RETRY = 2;
const BAD_GATEWAY = {
  status: 502,
  json: { error: { code: 'bad_gateway', message: 'bad gateway' } },
};
const BAD_REQUEST = { status: 400, json: { error: { code: 'bad_request', message: 'boom' } } };

type WireRequest = { method: string; path: string; headers: Record<string, string> };

function clearRequests(): WireRequest[] {
  return __http.requests.filter((r: WireRequest) => `${r.method} ${r.path}` === CLEAR);
}

function signedIn() {
  jest.spyOn(supabase.auth, 'getSession').mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  } as never);
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

function holdResponses() {
  const gate = deferred<void>();
  global.fetch = ((url: string, init: RequestInit) => {
    const response = fakeFetch(url, init);
    return gate.promise.then(() => response);
  }) as typeof fetch;
  return gate.resolve;
}

afterEach(() => {
  global.fetch = fakeFetch;
  jest.restoreAllMocks();
});

describe('useSettingsClearSearchHistory racing an in-flight history fetch', () => {
  beforeEach(() => {
    signedIn();
  });

  it('keeps the cleared list when a pre-clear fetch resolves after the optimistic clear', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const beforeClear = { items: [{ query: 'old search' }] };
    queryClient.setQueryData(discoveryKeys.history, beforeClear);
    const staleFetch = deferred<typeof beforeClear>();
    void queryClient
      .fetchQuery({ queryKey: discoveryKeys.history, queryFn: () => staleFetch.promise })
      .catch(() => undefined);
    __http.replyOnce(CLEAR, CLEARED);

    const hook = renderHook(() => useSettingsClearSearchHistory(), {
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

    expect(clearRequests()).toHaveLength(1);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    hook.unmount();
  });
});

describe('useSettingsClearSearchHistory when the clear fails', () => {
  beforeEach(() => {
    signedIn();
  });

  it('keeps the history items when the delete and the refetch both fail', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const items = [{ query: 'first' }, { query: 'second' }];
    queryClient.setQueryData(discoveryKeys.history, { items });
    queryClient.setQueryDefaults(discoveryKeys.history, {
      queryFn: () => Promise.reject(new Error('refetch failed')),
    });
    __http.reply(CLEAR, BAD_REQUEST);

    const hook = renderHook(() => useSettingsClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      hook.result.current.mutate();
    });
    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    await act(async () => {
      await new Promise((r) => setTimeout(r, 0));
    });

    expect(clearRequests()).toHaveLength(1);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items });
    hook.unmount();
  });
});

describe('settings mutations retry transient failures', () => {
  function renderClear() {
    const hook = renderHook(useSettingsClearSearchHistory, {
      wrapper: makeWrapper(new QueryClient()),
    });
    act(() => {
      hook.result.current.mutate();
    });
    return hook;
  }

  beforeEach(() => {
    jest.useFakeTimers();
    signedIn();
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  it('retries a 502 on clear-history and settles as success', async () => {
    __http.replyOnce(CLEAR, BAD_GATEWAY);
    __http.replyOnce(CLEAR, CLEARED);

    const hook = renderClear();
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_BASE_MS);
    });

    await waitFor(() => expect(hook.result.current.isSuccess).toBe(true));
    expect(clearRequests()).toHaveLength(ATTEMPT_AND_ONE_RETRY);
    hook.unmount();
  });

  it('does not retry a permanent 4xx on clear-history', async () => {
    __http.reply(CLEAR, BAD_REQUEST);

    const hook = renderClear();

    await waitFor(() => expect(hook.result.current.isError).toBe(true));
    expect(hook.result.current.error).toBeInstanceOf(ApiError);
    expect((hook.result.current.error as ApiError).status).toBe(BAD_REQUEST.status);
    expect(clearRequests()).toHaveLength(1);
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
    __http.reply(CLEAR, BAD_GATEWAY);
  });

  afterEach(() => {
    jest.useRealTimers();
  });

  describe('clear-history spreads its retry over the jittered backoff', () => {
    it.each(jitterSamples)(
      'reattempts at $dueMs ms when the jitter sample is $sample',
      async ({ sample, dueMs }) => {
        jest.spyOn(Math, 'random').mockReturnValue(sample);
        const queryClient = new QueryClient();
        const hook = renderHook(useSettingsClearSearchHistory, {
          wrapper: makeWrapper(queryClient),
        });
        act(() => {
          hook.result.current.mutate();
        });

        await elapse(dueMs - 1);
        expect(clearRequests()).toHaveLength(1);

        await elapse(1);
        expect(clearRequests()).toHaveLength(ATTEMPT_AND_ONE_RETRY);

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

  afterEach(() => {
    jest.useRealTimers();
  });

  it("a failed clear-history A started does not invalidate B's search history", async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    __http.replyOnce(CLEAR, BAD_REQUEST);
    const release = holdResponses();

    const clear = renderHook(() => useSettingsClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      clear.result.current.mutate();
    });
    await waitFor(() => expect(clearRequests()).toHaveLength(1));
    clear.unmount();

    await signOutThenSignInAsUserB(queryClient);
    const historyOfB = { items: [{ query: 'b searched this' }] };
    queryClient.setQueryData(discoveryKeys.history, historyOfB);
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');

    await act(async () => {
      release();
      await new Promise((r) => setTimeout(r, 0));
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
    __http.reply(CLEAR, BAD_GATEWAY);

    const clear = renderHook(() => useSettingsClearSearchHistory(), {
      wrapper: makeWrapper(queryClient),
    });
    act(() => {
      clear.result.current.mutate();
    });
    await waitFor(() => expect(clearRequests()).toHaveLength(1));
    clear.unmount();

    await signOutThenSignInAsUserB(queryClient);
    await act(async () => {
      await jest.advanceTimersByTimeAsync(RETRY_BACKOFF_BASE_MS);
    });

    expect(clearRequests().map((r) => r.headers.Authorization)).toEqual(['Bearer token-a']);

    session.unmount();
  });

  it('within one session the backfill and clear-history cache effects still apply', async () => {
    const queryClient = new QueryClient();
    const session = await bootAsUserA(queryClient);
    __http.reply(BACKFILL, {
      json: { scanned: 1, updated: 1, truncated: false, next_offset: 1 },
    });
    __http.reply(CLEAR, BAD_REQUEST);
    const tracksKey = [...libraryKeys.tracksPrefix, 'a'];
    queryClient.setQueryData(tracksKey, ['track-of-a']);
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'a' }] });

    const hooks = renderHook(
      () => ({ backfill: useBackfillFeatured(), clear: useSettingsClearSearchHistory() }),
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
