import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoverySearchHistoryResponse } from '@shared/apiClient/discovery';
import { ApiError, NetworkError } from '@shared/failure/errors';
import { discoveryKeys } from '@shared/query/queryKeys';
import { supabase } from '@shared/auth/supabaseClient';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { recordEvent } from '@shared/telemetry/recordEvent';
import {
  useDiscoverClearSearchHistory,
  useSearchHistory,
} from '@features/discovery/hooks/useSearchHistory';
import { DiscoverPortProvider } from '@features/discovery/ports/DiscoverPortProvider';
import type { DiscoverPort } from '@features/discovery/ports/discoverPort';
import { createMemoryDiscoverPort } from '@features/discovery/__tests__/memoryDiscoverPort';

jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));
jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));

const { __http } = require('@testing/doubles/fetch.js');

const CLEAR_HISTORY = 'DELETE /v1/discovery/search-history';
const NO_CONTENT = 204;
const BAD_REQUEST = 400;
const SERVER_ERROR = 500;
const TWO_CALLS = 2;
const BAD_GATEWAY = 502;

const mockRecordEvent = recordEvent as jest.Mock;

let queryClient: QueryClient;
let port: DiscoverPort;

function noAnswerYet(): void {}

interface WrapperProps {
  children: React.ReactNode;
}

function wrapper({ children }: WrapperProps) {
  return (
    <QueryClientProvider client={queryClient}>
      <DiscoverPortProvider port={port}>{children}</DiscoverPortProvider>
    </QueryClientProvider>
  );
}

function portListing(listSearchHistory: DiscoverPort['listSearchHistory']): DiscoverPort {
  return { ...createMemoryDiscoverPort(), listSearchHistory };
}

function clearRequests(): number {
  return __http.countFor(CLEAR_HISTORY);
}

function holdClearRequestsUntil(gate: Promise<void>): void {
  const realFetch = globalThis.fetch;
  async function heldFetch(input: RequestInfo | URL, init?: RequestInit): Promise<Response> {
    const response = realFetch(input, init);
    if (init?.method === 'DELETE') await gate;
    return response;
  }
  jest.spyOn(globalThis, 'fetch').mockImplementation(heldFetch);
}

function failureEvents() {
  return mockRecordEvent.mock.calls
    .map((call) => call[0] as { type: string; payload?: Record<string, unknown> })
    .filter((event) => event.type === 'search_failed');
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  port = createMemoryDiscoverPort();
  jest.mocked(supabase.auth.getSession).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  } as Awaited<ReturnType<typeof supabase.auth.getSession>>);
  mockRecordEvent.mockReset().mockResolvedValue(undefined);
});

afterEach(() => {
  queryClient.clear();
  jest.restoreAllMocks();
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  it('a failed history fetch reports the correlation id of a transport failure', async () => {
    const transportFailure = new NetworkError('transport', 'offline', 'f0e1d2c3b4a59687');
    port = portListing(() => Promise.reject(transportFailure));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({
      source: 'history',
      correlationId: 'f0e1d2c3b4a59687',
    });
  });

  it('a failed history fetch fires search_failed with source history', async () => {
    port = portListing(() => Promise.reject(new ApiError(SERVER_ERROR, 'boom')));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({ source: 'history', status: SERVER_ERROR });
  });

  it('a successful query emits no failure event', async () => {
    port = createMemoryDiscoverPort({ history: ['radiohead'] });
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.data?.items[0]?.query).toBe('radiohead'));

    expect(failureEvents()).toHaveLength(0);
  });

  it('one failure is reported once, across re-renders and a second observer of the same query', async () => {
    const failure = new ApiError(SERVER_ERROR, 'boom');
    port = portListing(() => Promise.reject(failure));
    const first = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(first.result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    first.rerender({});
    const second = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(second.result.current.error).not.toBeNull());

    expect(failureEvents()).toHaveLength(1);
  });

  it('a later, separate failure of the same query is reported again', async () => {
    let historyFailure = new ApiError(SERVER_ERROR, 'boom');
    port = portListing(() => Promise.reject(historyFailure));
    renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    historyFailure = new ApiError(BAD_GATEWAY, 'bad gateway');
    await queryClient.refetchQueries({ queryKey: discoveryKeys.history });

    await waitFor(() => expect(failureEvents()).toHaveLength(2));
    expect(failureEvents()[1]?.payload).toEqual({ source: 'history', status: BAD_GATEWAY });
  });
});

describe('useClearSearchHistory empties the cache at once and rolls it back on failure', () => {
  function setup() {
    queryClient = new QueryClient({
      defaultOptions: {
        queries: { gcTime: Infinity },
        mutations: { retry: false, gcTime: Infinity },
      },
    });
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'old' }] });
    const invalidate = jest.spyOn(queryClient, 'invalidateQueries');
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });
    return { queryClient, invalidate, result, clear: result.current.clear };
  }

  it('writes an empty history before the server call and does not refetch on success', async () => {
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(clearRequests()).toBe(1));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(__http.last().path).toBe('/v1/discovery/search-history');
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('rolls the history back, surfaces the error and invalidates when the server call fails', async () => {
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    const { queryClient, invalidate, result, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(result.current.error).toBeInstanceOf(ApiError));
    expect(result.current.error).toMatchObject({ status: BAD_REQUEST });
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [{ query: 'old' }] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: discoveryKeys.history });
  });

  it('leaves the error null on success and clears a previous failure on retry', async () => {
    __http.replyOnce(CLEAR_HISTORY, { status: BAD_REQUEST });
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    const { queryClient, result } = setup();

    expect(result.current.error).toBeNull();
    act(() => result.current.clear());
    await waitFor(() => expect(result.current.error).not.toBeNull());

    act(() => result.current.clear());
    await waitFor(() => expect(clearRequests()).toBe(TWO_CALLS));
    await waitFor(() => expect(result.current.error).toBeNull());
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
  });

  it('does not restore or refetch history when the failure settles after sign-out', async () => {
    let answer = noAnswerYet;
    holdClearRequestsUntil(
      new Promise<void>((resolve) => {
        answer = resolve;
      }),
    );
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(clearRequests()).toBe(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      answer();
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('does not empty the history when the success settles after sign-out', async () => {
    let answer = noAnswerYet;
    holdClearRequestsUntil(
      new Promise<void>((resolve) => {
        answer = resolve;
      }),
    );
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    const { queryClient, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(clearRequests()).toBe(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      answer();
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
  });
});

describe('discover clear-history racing an unrelated history invalidation', () => {
  const PRE_CLEAR_HISTORY: DiscoverySearchHistoryResponse = {
    items: [{ query: 'old search', query_norm: 'old search', executed_at: '2026-01-01T00:00:00Z' }],
    total: 1,
  };
  const CLEARED_HISTORY: DiscoverySearchHistoryResponse = { items: [], total: 0 };

  function deferred<T>() {
    let resolve!: (value: T) => void;
    const promise = new Promise<T>((res) => {
      resolve = res;
    });
    return { promise, resolve };
  }

  function serverHoldingHistory(refetchAnswers: Promise<DiscoverySearchHistoryResponse>[] = []) {
    const commit = deferred<void>();
    let history = PRE_CLEAR_HISTORY;
    let reads = 0;
    const answers = [...refetchAnswers];
    holdClearRequestsUntil(
      commit.promise.then(() => {
        history = CLEARED_HISTORY;
      }),
    );
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    function readHistory(): Promise<DiscoverySearchHistoryResponse> {
      reads += 1;
      if (reads > 1 && answers.length > 0) return answers.shift()!;
      return Promise.resolve(history);
    }
    port = portListing(readHistory);
    return {
      commitClear: commit.resolve,
      historyReads: (): number => reads,
    };
  }

  async function renderHistoryWithClear(client: QueryClient) {
    queryClient = client;
    const screen = renderHook(
      () => ({ history: useSearchHistory(), clear: useDiscoverClearSearchHistory() }),
      { wrapper },
    );
    await waitFor(() => expect(screen.result.current.history.data).toEqual(PRE_CLEAR_HISTORY));
    return screen;
  }

  function cachedHistoryItems(queryClient: QueryClient) {
    return queryClient.getQueryData<DiscoverySearchHistoryResponse>(discoveryKeys.history)?.items;
  }

  async function flushPendingCacheWrites() {
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 0));
    });
  }

  it('leaves no history when a search-driven refetch repopulated the cache mid-clear', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const server = serverHoldingHistory();
    const screen = await renderHistoryWithClear(queryClient);

    act(() => screen.result.current.clear.clear());
    await waitFor(() => expect(clearRequests()).toBe(1));
    await act(async () => {
      await queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    });
    expect(cachedHistoryItems(queryClient)).toEqual(PRE_CLEAR_HISTORY.items);
    await act(async () => {
      server.commitClear();
    });
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    await flushPendingCacheWrites();

    expect(cachedHistoryItems(queryClient)).toEqual([]);
    expect(screen.result.current.history.data?.items).toEqual([]);
    screen.unmount();
  });

  it('leaves no history when that refetch is still in flight as the clear commits', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const racingRefetch = deferred<DiscoverySearchHistoryResponse>();
    const server = serverHoldingHistory([racingRefetch.promise]);
    const screen = await renderHistoryWithClear(queryClient);

    act(() => screen.result.current.clear.clear());
    await waitFor(() => expect(clearRequests()).toBe(1));
    void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    await waitFor(() => expect(server.historyReads()).toBe(TWO_CALLS));
    await act(async () => {
      server.commitClear();
    });
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    await act(async () => {
      racingRefetch.resolve(PRE_CLEAR_HISTORY);
      await racingRefetch.promise;
    });
    await flushPendingCacheWrites();

    expect(cachedHistoryItems(queryClient)).toEqual([]);
    expect(screen.result.current.history.data?.items).toEqual([]);
    screen.unmount();
  });
});

describe('discover clear-history retries transient failures', () => {
  const EXISTING_HISTORY = { items: [{ query: 'old' }] };

  function setup() {
    queryClient = new QueryClient({ defaultOptions: { mutations: { retryDelay: 0 } } });
    queryClient.setQueryData(discoveryKeys.history, EXISTING_HISTORY);
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });
    return { queryClient, result };
  }

  it('keeps the history cleared and the error unset when a 502 is followed by a success', async () => {
    __http.replyOnce(CLEAR_HISTORY, { status: BAD_GATEWAY });
    __http.reply(CLEAR_HISTORY, { status: NO_CONTENT });
    const { queryClient, result } = setup();

    act(() => result.current.clear());

    await waitFor(() => expect(clearRequests()).toBe(TWO_CALLS));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(result.current.error).toBeNull();
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
  });

  it('rolls back and surfaces a permanent 400 without reattempting it', async () => {
    __http.reply(CLEAR_HISTORY, { status: BAD_REQUEST });
    const { queryClient, result } = setup();

    act(() => result.current.clear());

    await waitFor(() => expect(result.current.error).toBeInstanceOf(ApiError));
    expect(result.current.error).toMatchObject({ status: BAD_REQUEST });
    expect(clearRequests()).toBe(1);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(EXISTING_HISTORY);
  });
});
