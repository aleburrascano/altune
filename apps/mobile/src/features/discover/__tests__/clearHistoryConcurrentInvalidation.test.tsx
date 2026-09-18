import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import {
  clearSearchHistory,
  listSearchHistory,
  type DiscoverySearchHistoryResponse,
} from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';

import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useSearchHistory } from '../hooks/useSearchHistory';

// #1679: useDiscoverLogic invalidates the history key whenever a search settles,
// with no knowledge of an in-flight clear. That refetch reads the pre-clear list
// from a server that has not committed the clear yet, so the clear has to be the
// last write to the cache once it does commit.

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
  listSearchHistory: jest.fn(),
}));

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

describe('discover clear-history racing an unrelated history invalidation (#1679)', () => {
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
