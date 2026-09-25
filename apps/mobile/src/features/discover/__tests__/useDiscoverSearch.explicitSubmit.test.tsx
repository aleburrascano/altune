import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { renderHook, waitFor } from '@testing-library/react-native';

import { searchDiscovery, type DiscoverySearchResponse } from '@shared/api-client/discovery';
import { useDiscoverSearch } from '../hooks/useDiscoverSearch';

jest.mock('@shared/api-client/discovery', () => ({ searchDiscovery: jest.fn() }));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const mockSearch = searchDiscovery as jest.Mock;

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

function singlePage(): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 0,
    offset: 0,
    has_more: false,
  };
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, staleTime: 30_000 } },
  });
  mockSearch.mockReset().mockImplementation(() => Promise.resolve(singlePage()));
});

afterEach(() => {
  queryClient.clear();
});

describe('an explicit submit of a query the debounce just fetched', () => {
  it('still sends a request with saveHistory true', async () => {
    const { result, rerender } = renderHook(
      ({ saveHistory }: { saveHistory: boolean }) => useDiscoverSearch('radiohead', saveHistory),
      { wrapper, initialProps: { saveHistory: false } },
    );
    await waitFor(() => expect(result.current.data).toBeDefined());

    rerender({ saveHistory: true });

    await waitFor(() =>
      expect(mockSearch).toHaveBeenCalledWith(
        expect.objectContaining({ q: 'radiohead', saveHistory: true }),
        expect.anything(),
      ),
    );
  });
});
