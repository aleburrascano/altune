import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';
import { useDiscoverLogic } from '@features/discovery/hooks/useDiscoverLogic';
import { setSearchState } from '@features/discovery/domain/searchState';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { signInTestUser } from '@testing/signInTestUser';
import { FeaturePorts } from '@/wiring/FeaturePorts';
import { httpFeaturePorts } from '@/wiring/httpFeaturePorts';

jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const { __http } = require('@testing/doubles/fetch.js');

const SEARCH = 'GET /v1/discovery/search';
const SUGGEST = 'GET /v1/discovery/suggest';
const HISTORY_LIST = 'GET /v1/discovery/search-history';
const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

function searchResponse(partial: boolean): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture()],
    sections: [],
    providers: [
      { provider: 'spotify', status: 'ok', result_count: 1, latency_ms: 80 },
      { provider: 'deezer', status: partial ? 'timeout' : 'ok', result_count: 0, latency_ms: 3000 },
    ],
    partial,
    cache: { hit: false, fetched_at: null },
    total: 1,
    offset: 0,
    has_more: false,
  };
}

interface RecordedRequest {
  path: string;
  body: string;
}

interface RecordedEvent {
  type: string;
}

function degradedEvents(): RecordedEvent[] {
  return (__http.requests as RecordedRequest[])
    .filter((request) => request.path === EVENTS_PATH)
    .map((request) => JSON.parse(request.body) as RecordedEvent)
    .filter((event) => event.type === 'search_degraded');
}

beforeEach(async () => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  await signInTestUser();
  __http.reply(SUGGEST, { json: { suggestions: [] } });
  __http.reply(HISTORY_LIST, { json: { items: [] } });
  __http.reply(EVENTS, { status: 204 });
  setSearchState('radiohead', 'radiohead');
});

afterEach(() => {
  __http.reset();
  queryClient.clear();
  setSearchState('', '');
});

describe('useDiscoverLogic surfaces a partial (degraded) search to the UI and telemetry', () => {
  it('marks a partial response incomplete and records one search_degraded event', async () => {
    __http.reply(SEARCH, { json: searchResponse(true) });

    const { result, rerender } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.results.view).toBe('results'));
    expect(result.current.results.resultsIncomplete).toBe(true);
    await waitFor(() => expect(degradedEvents()).toHaveLength(1));
    expect(degradedEvents()[0]).toEqual({
      type: 'search_degraded',
      search_id: 'search-1',
      payload: {
        session_id: expect.any(String),
        result_count: 1,
        degraded_providers: [{ provider: 'deezer', status: 'timeout' }],
      },
    });

    rerender({});
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(degradedEvents()).toHaveLength(1);
  });

  it('reports a provider that only degrades on the second page', async () => {
    const healthyFirstPage = { ...searchResponse(false), has_more: true };
    const degradedSecondPage = { ...searchResponse(true), offset: 1 };
    __http.replyOnce(SEARCH, { json: healthyFirstPage });
    __http.reply(SEARCH, { json: degradedSecondPage });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.results.view).toBe('results'));
    expect(degradedEvents()).toHaveLength(0);

    act(() => {
      result.current.pagination.onEndReached();
    });

    await waitFor(() => expect(result.current.results.resultsIncomplete).toBe(true));
    await waitFor(() => expect(degradedEvents()).toHaveLength(1));
    expect(degradedEvents()[0]).toEqual({
      type: 'search_degraded',
      search_id: 'search-1',
      payload: {
        session_id: expect.any(String),
        result_count: 2,
        degraded_providers: [{ provider: 'deezer', status: 'timeout' }],
      },
    });
  });

  it('leaves a healthy response complete and records no search_degraded event', async () => {
    __http.reply(SEARCH, { json: searchResponse(false) });

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });

    await waitFor(() => expect(result.current.results.view).toBe('results'));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
    expect(result.current.results.resultsIncomplete).toBe(false);
    expect(degradedEvents()).toHaveLength(0);
  });
});
