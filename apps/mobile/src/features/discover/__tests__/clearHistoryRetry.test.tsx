import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { ApiError } from '@shared/api-client';
import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';

// #1678: mutations default to zero retries, so a transient blip rolled discover's
// history back and surfaced an error while settings' copy of the same DELETE recovered.

jest.mock('@shared/api-client/discovery', () => ({
  clearSearchHistory: jest.fn(),
}));

const mockClearSearchHistory = clearSearchHistory as jest.Mock;

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

describe('discover clear-history retries transient failures (#1678)', () => {
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
