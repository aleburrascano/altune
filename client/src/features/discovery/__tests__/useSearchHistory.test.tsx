import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  searchDiscovery,
  suggestDiscovery,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { ApiError, NetworkError } from '@shared/errors';
import { discoveryKeys } from '@shared/query/queryKeys';
import { runSignOutCleanups } from '@shared/session/signOutCleanup';
import { recordEvent } from '@shared/telemetry/recordEvent';
import {
  useDiscoverClearSearchHistory,
  useSearchHistory,
} from '@features/discovery/hooks/useSearchHistory';

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  searchDiscovery: jest.fn(),
  suggestDiscovery: jest.fn(),
  listSearchHistory: jest.fn(),
  clearSearchHistory: jest.fn(),
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;
const mockSuggest = suggestDiscovery as jest.Mock;
const mockHistory = listSearchHistory as jest.Mock;
const mockRecordEvent = recordEvent as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
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
  mockSearch.mockReset();
  mockSuggest.mockReset();
  mockHistory.mockReset();
  mockRecordEvent.mockReset().mockResolvedValue(undefined);
});

afterEach(() => {
  queryClient.clear();
});

describe('discover query failures emit a search_failed telemetry event tagged with its source', () => {
  it('a failed history fetch reports the correlation id of a transport failure', async () => {
    mockHistory.mockRejectedValue(new NetworkError('transport', 'offline', 'f0e1d2c3b4a59687'));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({
      source: 'history',
      correlationId: 'f0e1d2c3b4a59687',
    });
  });

  it('a failed history fetch fires search_failed with source history', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    expect(failureEvents()[0]?.payload).toEqual({ source: 'history', status: 500 });
  });

  it('a successful query emits no failure event', async () => {
    mockHistory.mockResolvedValue({ items: [] });
    const { result } = renderHook(() => useSearchHistory(), { wrapper });

    await waitFor(() => expect(result.current.data).toEqual({ items: [] }));

    expect(failureEvents()).toHaveLength(0);
  });

  it('one failure is reported once, across re-renders and a second observer of the same query', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    const first = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(first.result.current.error).not.toBeNull());
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    first.rerender({});
    const second = renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(second.result.current.error).not.toBeNull());

    expect(failureEvents()).toHaveLength(1);
  });

  it('a later, separate failure of the same query is reported again', async () => {
    mockHistory.mockRejectedValue(new ApiError(500, 'boom'));
    renderHook(() => useSearchHistory(), { wrapper });
    await waitFor(() => expect(failureEvents()).toHaveLength(1));

    mockHistory.mockRejectedValue(new ApiError(502, 'bad gateway'));
    await queryClient.refetchQueries({ queryKey: discoveryKeys.history });

    await waitFor(() => expect(failureEvents()).toHaveLength(2));
    expect(failureEvents()[1]?.payload).toEqual({ source: 'history', status: 502 });
  });
});

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
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });
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
      () => ({ history: useSearchHistory(), clear: useDiscoverClearSearchHistory() }),
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
  const EXISTING_HISTORY = { items: [{ query: 'old' }] };

  let queryClient: QueryClient;

  function setup() {
    queryClient = new QueryClient({ defaultOptions: { mutations: { retryDelay: 0 } } });
    queryClient.setQueryData(discoveryKeys.history, EXISTING_HISTORY);
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });
    return { queryClient, result };
  }

  beforeEach(() => {
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
