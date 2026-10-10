import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import { useRefreshFromFirstPage } from '@features/discovery/hooks/useSearchRefresh';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/recordEvent', () => ({ recordEvent: jest.fn() }));

const SEARCH = 'GET /v1/discovery/search';

type SentRequest = { path: string; query: string };

const SECOND_PAGE_OFFSET = 3;
const TWO_PAGES_AND_A_RESTART = 3;
const RESTART_AND_ITS_RETRY = 4;

function searchRequests(): SentRequest[] {
  return (__http.requests as SentRequest[]).filter(
    (request) => request.path === '/v1/discovery/search',
  );
}

function servePages(...pages: DiscoverySearchResponse[]): void {
  for (const page of pages) __http.replyOnce(SEARCH, { json: page });
}
let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

function pageOf(offset: number, searchId: string, titles: string[]): DiscoverySearchResponse {
  return {
    query: 'q',
    query_norm: 'q',
    search_id: searchId,
    results: titles.map((title) => resultFixture({ title })),
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 9,
    offset,
    has_more: true,
  };
}

beforeEach(() => {
  queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
});

afterEach(() => {
  queryClient.clear();
  jest.restoreAllMocks();
});

describe('held-slate expiry', () => {
  it('restarts from page 1 instead of stitching a fresh ranking onto old pages', async () => {
    servePages(
      pageOf(0, 'search-1', ['A1', 'A2', 'A3']),
      pageOf(SECOND_PAGE_OFFSET, 'search-2', ['B1', 'B2', 'B3']),
      pageOf(0, 'search-3', ['C1', 'C2', 'C3']),
    );
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.fetchNextPage();
    });

    await waitFor(() => expect(searchRequests()).toHaveLength(TWO_PAGES_AND_A_RESTART));
    await waitFor(() =>
      expect(result.current.data?.results.map((r) => r.title)).toEqual(['C1', 'C2', 'C3']),
    );

    const titles = result.current.data?.results.map((r) => r.title) ?? [];
    expect(new Set(titles).size).toBe(titles.length);
    expect(titles).not.toEqual(expect.arrayContaining(['A1', 'B1']));
    expect(new URLSearchParams(searchRequests()[1]?.query).get('search_id')).toBe('search-1');
    expect(new URLSearchParams(searchRequests()[2]?.query).has('search_id')).toBe(false);
    expect(new URLSearchParams(searchRequests()[2]?.query).has('offset')).toBe(false);
  });
});

describe('held-slate expiry when the restart fails', () => {
  it('restarts once and keeps showing only the pages from the original search', async () => {
    servePages(
      pageOf(0, 'search-1', ['A1', 'A2', 'A3']),
      pageOf(SECOND_PAGE_OFFSET, 'search-2', ['B1', 'B2', 'B3']),
    );
    __http.reply(SEARCH, { status: 503, json: { detail: 'search unavailable' } });
    const { result } = renderHook(() => useDiscoverSearch('q'), { wrapper });
    await waitFor(() => expect(result.current.data).toBeDefined());

    await act(async () => {
      await result.current.fetchNextPage();
    });
    await waitFor(() => expect(result.current.refreshFailed).toBe(true));
    await act(async () => {
      await new Promise((resolve) => setTimeout(resolve, 200));
    });

    expect(searchRequests()).toHaveLength(RESTART_AND_ITS_RETRY);
    expect(result.current.data?.results.map((r) => r.title)).toEqual(['A1', 'A2', 'A3']);
  });
});

const queryKey = ['discovery', 'search', 'q', true];

function pagesOf(title: string) {
  return { pages: [{ results: [{ title }] }], pageParams: [{ offset: 0 }] };
}

describe('useRefreshFromFirstPage', () => {
  it('leaves no held slate when the refetch rejects', async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(queryKey, pagesOf('old-row'));
    const wrapper = ({ children }: { children: React.ReactNode }) => (
      <QueryClientProvider client={queryClient}>
        <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
      </QueryClientProvider>
    );
    const refetch = jest.fn().mockRejectedValue(new Error('offline'));
    const { result } = renderHook(() => useRefreshFromFirstPage(queryKey, refetch), { wrapper });

    await act(async () => {
      await result.current.refresh().catch(() => undefined);
    });

    expect(result.current.held).toBeUndefined();
  });
});
