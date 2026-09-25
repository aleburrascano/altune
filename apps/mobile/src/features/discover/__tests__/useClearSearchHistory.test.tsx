import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import {
  clearSearchHistory,
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { discoveryKeys } from '@shared/lib/query-keys';

import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useSearchHistory } from '../hooks/useSearchHistory';

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
  listSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: jest.fn() }),
}));

const mockClearSearchHistory = clearSearchHistory as jest.Mock;

describe('useClearSearchHistory empties the cache at once and rolls it back on failure', () => {
  beforeEach(() => {
    jest.clearAllMocks();
  });

  let queryClient: QueryClient;

  afterEach(() => {
    queryClient.clear();
  });

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
    const { result } = renderHook(() => useClearSearchHistory(), { wrapper });
    return { queryClient, invalidate, result, clear: result.current.clear };
  }

  it('writes an empty history before the server call and does not refetch on success', async () => {
    mockClearSearchHistory.mockResolvedValue(undefined);
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('rolls the history back, surfaces the error and invalidates when the server call fails', async () => {
    const failure = new Error('offline');
    mockClearSearchHistory.mockRejectedValue(failure);
    const { queryClient, invalidate, result, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(result.current.error).toBe(failure));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [{ query: 'old' }] });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: discoveryKeys.history });
  });

  it('leaves the error null on success and clears a previous failure on retry', async () => {
    mockClearSearchHistory.mockRejectedValueOnce(new Error('offline'));
    mockClearSearchHistory.mockResolvedValueOnce(undefined);
    const { queryClient, result } = setup();

    expect(result.current.error).toBeNull();
    act(() => result.current.clear());
    await waitFor(() => expect(result.current.error).not.toBeNull());

    act(() => result.current.clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.error).toBeNull());
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
  });

  it('does not restore or refetch history when the failure settles after sign-out', async () => {
    let reject: (e: Error) => void = () => undefined;
    mockClearSearchHistory.mockReturnValue(
      new Promise<void>((_resolve, rej) => {
        reject = rej;
      }),
    );
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      reject(new Error('offline'));
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('does not empty the history when the success settles after sign-out', async () => {
    let resolve: () => void = () => undefined;
    mockClearSearchHistory.mockReturnValue(
      new Promise<void>((res) => {
        resolve = res;
      }),
    );
    const { queryClient, clear } = setup();

    act(() => clear());
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    runSignOutCleanups();
    queryClient.setQueryData(discoveryKeys.history, { items: [{ query: 'next-user' }] });
    await act(async () => {
      resolve();
      await Promise.resolve();
    });

    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({
      items: [{ query: 'next-user' }],
    });
  });
});

describe('discover clear-history racing an unrelated history invalidation', () => {
  // #1679: useDiscoverLogic invalidates the history key whenever a search settles,
  // with no knowledge of an in-flight clear. That refetch reads the pre-clear list
  // from a server that has not committed the clear yet, so the clear has to be the
  // last write to the cache once it does commit.

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

  /** A server whose history only empties once the clear call it handed out commits. */
  function serverHoldingHistory() {
    const commit = deferred<void>();
    let history = PRE_CLEAR_HISTORY;
    jest.mocked(clearSearchHistory).mockImplementation(() =>
      commit.promise.then(() => {
        history = CLEARED_HISTORY;
      }),
    );
    return {
      commitClear: commit.resolve,
      readHistory: (): DiscoverySearchHistoryResponse => history,
    };
  }

  function makeWrapper(queryClient: QueryClient) {
    return function Wrapper({ children }: { children: React.ReactNode }) {
      return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
    };
  }

  async function renderHistoryWithClear(queryClient: QueryClient) {
    const screen = renderHook(
      () => ({ history: useSearchHistory(), clear: useClearSearchHistory() }),
      { wrapper: makeWrapper(queryClient) },
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

  beforeEach(() => {
    jest.mocked(clearSearchHistory).mockReset();
    jest.mocked(listSearchHistory).mockReset();
  });

  it('leaves no history when a search-driven refetch repopulated the cache mid-clear', async () => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const server = serverHoldingHistory();
    jest.mocked(listSearchHistory).mockImplementation(async () => server.readHistory());
    const screen = await renderHistoryWithClear(queryClient);

    act(() => screen.result.current.clear.clear());
    await waitFor(() => expect(clearSearchHistory).toHaveBeenCalledTimes(1));
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
    const server = serverHoldingHistory();
    const racingRefetch = deferred<DiscoverySearchHistoryResponse>();
    jest
      .mocked(listSearchHistory)
      .mockResolvedValueOnce(PRE_CLEAR_HISTORY)
      .mockReturnValueOnce(racingRefetch.promise)
      .mockImplementation(async () => server.readHistory());
    const screen = await renderHistoryWithClear(queryClient);

    act(() => screen.result.current.clear.clear());
    await waitFor(() => expect(clearSearchHistory).toHaveBeenCalledTimes(1));
    void queryClient.invalidateQueries({ queryKey: discoveryKeys.history });
    await waitFor(() => expect(listSearchHistory).toHaveBeenCalledTimes(2));
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
  // #1678: mutations default to zero retries, so a transient blip rolled discover's
  // history back and surfaced an error while settings' copy of the same DELETE recovered.

  const EXISTING_HISTORY = { items: [{ query: 'old' }] };

  let queryClient: QueryClient;

  function setup() {
    // retryDelay only keeps the test fast; the retry decision comes from the hook.
    queryClient = new QueryClient({ defaultOptions: { mutations: { retryDelay: 0 } } });
    queryClient.setQueryData(discoveryKeys.history, EXISTING_HISTORY);
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useClearSearchHistory(), { wrapper });
    return { queryClient, result };
  }

  beforeEach(() => {
    // Reset, not clear: a queued *Once outcome left unconsumed by a failing test
    // would otherwise decide the next one.
    mockClearSearchHistory.mockReset();
  });

  afterEach(() => {
    queryClient.clear();
  });

  it('keeps the history cleared and the error unset when a 502 is followed by a success', async () => {
    mockClearSearchHistory
      .mockRejectedValueOnce(new ApiError(502, 'bad gateway'))
      .mockResolvedValueOnce(undefined);
    const { queryClient, result } = setup();

    act(() => result.current.clear());

    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(queryClient.isMutating()).toBe(0));
    expect(result.current.error).toBeNull();
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
  });

  it('rolls back and surfaces a permanent 400 without reattempting it', async () => {
    const rejection = new ApiError(400, 'bad request');
    mockClearSearchHistory.mockRejectedValue(rejection);
    const { queryClient, result } = setup();

    act(() => result.current.clear());

    await waitFor(() => expect(result.current.error).toBe(rejection));
    expect(mockClearSearchHistory).toHaveBeenCalledTimes(1);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(EXISTING_HISTORY);
  });
});
