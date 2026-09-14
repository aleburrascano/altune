import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';

import { useClearSearchHistory } from '../hooks/useClearSearchHistory';

// #839: a history refetch already in flight when the user taps "Clear" must not
// resolve on top of the optimistic empty list and repopulate it.

jest.mock('@shared/api-client/discovery', () => ({
  ...jest.requireActual('@shared/api-client/discovery'),
  clearSearchHistory: jest.fn(),
}));

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolve = res;
  });
  return { promise, resolve };
}

function makeWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: React.ReactNode }) {
    return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
  };
}

describe('useClearSearchHistory racing an in-flight history fetch (#839)', () => {
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
