import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';
import { supabase } from '@shared/auth/supabaseClient';
import { createMemoryFileStore } from '@shared/files/__tests__/memoryFileStore';
import { applyKillSwitches, setKillSwitchFileStore } from '@shared/killSwitch/killSwitch';
import { discoveryKeys } from '@shared/query/queryKeys';

import { useAutocompleteSuggestions } from '@features/discovery/hooks/useSuggestions';
import { useDiscoverLogic } from '@features/discovery/hooks/useDiscoverLogic';
import { useDiscoverSearch } from '@features/discovery/hooks/useDiscoverSearch';
import {
  useDiscoverClearSearchHistory,
  useSearchHistory,
} from '@features/discovery/hooks/useSearchHistory';
import { setSearchState } from '@features/discovery/domain/searchState';
import { FeaturePorts } from '@/chrome/FeaturePorts';
import { httpFeaturePorts } from '@/chrome/httpFeaturePorts';

const { __http } = require('@testing/doubles/fetch.js');

jest.mock('@shared/auth/supabaseClient', () => ({
  supabase: { auth: { getSession: jest.fn() } },
}));
jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: jest.fn() }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: jest.fn() }),
  useFocusEffect: jest.fn(),
}));

const SEARCH = 'GET /v1/discovery/search';
const SUGGEST = 'GET /v1/discovery/suggest';
const HISTORY_LIST = 'GET /v1/discovery/search-history';
const HISTORY_CLEAR = 'DELETE /v1/discovery/search-history';

const QUERY = 'radiohead';
const HISTORY = {
  items: [{ query: QUERY, query_norm: QUERY, executed_at: '2026-01-01T00:00:00Z' }],
  total: 1,
};

const SEARCH_AND_ONE_REFETCH = 2;

function sent(spec: string): number {
  return __http.countFor(spec);
}

function searchResponseFixture(): DiscoverySearchResponse {
  return {
    query: QUERY,
    query_norm: QUERY,
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

let queryClient: QueryClient;

function wrapper({ children }: { children: React.ReactNode }) {
  return (
    <QueryClientProvider client={queryClient}>
      <FeaturePorts ports={httpFeaturePorts}>{children}</FeaturePorts>
    </QueryClientProvider>
  );
}

function switchDiscoveryFetches(enabled: boolean): void {
  act(() => applyKillSwitches({ discovery_enabled: enabled }));
}

async function settle(): Promise<void> {
  await act(async () => {
    await Promise.resolve();
  });
}

beforeEach(() => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false, retryDelay: 0 } },
  });
  setKillSwitchFileStore(createMemoryFileStore());
  jest.spyOn(console, 'warn').mockImplementation(() => undefined);
  (supabase.auth.getSession as jest.Mock).mockResolvedValue({
    data: { session: { access_token: 'tok' } },
    error: null,
  });
  __http.reply(SEARCH, { json: searchResponseFixture() });
  __http.reply(SUGGEST, { json: { suggestions: [] } });
  __http.reply(HISTORY_LIST, { json: HISTORY });
  __http.reply(HISTORY_CLEAR, { status: 204 });
  setSearchState('', QUERY);
});

afterEach(() => {
  queryClient.clear();
  setSearchState('', '');
  setKillSwitchFileStore();
  jest.restoreAllMocks();
});

type GatedHookState = { data: unknown };

type GatedHookCase = {
  hook: string;
  useGatedHook: () => GatedHookState;
  request: string;
};

const gatedHooks: GatedHookCase[] = [
  { hook: 'useDiscoverSearch', useGatedHook: () => useDiscoverSearch(QUERY), request: SEARCH },
  {
    hook: 'useAutocompleteSuggestions',
    useGatedHook: () => useAutocompleteSuggestions(QUERY),
    request: SUGGEST,
  },
  { hook: 'useSearchHistory', useGatedHook: () => useSearchHistory(), request: HISTORY_LIST },
];

type SearchHook = ReturnType<typeof useDiscoverSearch>;

const searchAffordances: { affordance: string; tap: (search: SearchHook) => void }[] = [
  { affordance: 'retry', tap: (search) => search.refetch() },
  {
    affordance: 'next page',
    tap: (search) => {
      void search.fetchNextPage();
    },
  },
];

describe('discover fetches — remote kill switch', () => {
  it.each(gatedHooks)(
    '$hook fires no request while the switch is off',
    async ({ useGatedHook, request }) => {
      switchDiscoveryFetches(false);

      const { result } = renderHook(useGatedHook, { wrapper });
      await settle();

      expect(sent(request)).toBe(0);
      expect(result.current.data).toBeUndefined();
    },
  );

  it.each(searchAffordances)(
    'ignores the $affordance affordance tapped while the switch is off',
    async ({ tap }) => {
      switchDiscoveryFetches(false);
      const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });
      await settle();

      await act(async () => {
        tap(result.current);
        await Promise.resolve();
      });

      expect(sent(SEARCH)).toBe(0);
      expect(result.current.isUnavailable).toBe(true);
    },
  );

  it('keeps the cached history when a clear is tapped while the switch is off', async () => {
    switchDiscoveryFetches(false);
    queryClient.setQueryData(discoveryKeys.history, HISTORY);
    const { result } = renderHook(() => useDiscoverClearSearchHistory(), { wrapper });

    act(() => result.current.clear());
    await settle();

    expect(sent(HISTORY_CLEAR)).toBe(0);
    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual(HISTORY);
  });

  it('tells the screen search is unavailable instead of searching while the switch is off', async () => {
    switchDiscoveryFetches(false);

    const { result } = renderHook(() => useDiscoverLogic(), { wrapper });
    await settle();

    expect(result.current.results.view).toBe('unavailable');
    expect(result.current.suggestionItems).toEqual([]);
    expect(result.current.history.items).toEqual([]);
  });

  it('searches again when the switch is turned back on, without a remount', async () => {
    switchDiscoveryFetches(false);
    const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });
    await settle();
    expect(sent(SEARCH)).toBe(0);

    switchDiscoveryFetches(true);

    await waitFor(() => expect(result.current.data?.query).toBe(QUERY));
    expect(sent(SEARCH)).toBe(1);
  });

  it('leaves discover searching while another loop is switched off', async () => {
    act(() => applyKillSwitches({ sse_enabled: false, offline_downloads_enabled: false }));

    const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });

    await waitFor(() => expect(result.current.data?.query).toBe(QUERY));
    expect(sent(SEARCH)).toBe(1);
  });

  it('keeps searching after another loop is switched off with the screen mounted', async () => {
    const { result } = renderHook(() => useDiscoverSearch(QUERY), { wrapper });
    await waitFor(() => expect(result.current.data?.query).toBe(QUERY));
    expect(sent(SEARCH)).toBe(1);

    act(() => applyKillSwitches({ sse_enabled: false }));
    act(() => result.current.refetch());

    await waitFor(() => expect(sent(SEARCH)).toBe(SEARCH_AND_ONE_REFETCH));
    await waitFor(() => expect(queryClient.isFetching()).toBe(0));
  });

  describe('history retry', () => {
    async function failHistoryLoad() {
      __http.reset();
      __http.reply(SEARCH, { json: searchResponseFixture() });
      __http.reply(SUGGEST, { json: { suggestions: [] } });
      __http.replyOnce(HISTORY_LIST, { status: 503, json: { detail: 'history down' } });
      __http.reply(HISTORY_LIST, { json: HISTORY });
      const hook = renderHook(() => useDiscoverLogic(), { wrapper });
      await waitFor(() => expect(hook.result.current.history.failed).toBe(true));
      return hook;
    }

    it('sends no request when tapped while the switch is off', async () => {
      const { result } = await failHistoryLoad();
      const sentBefore = sent(HISTORY_LIST);
      switchDiscoveryFetches(false);

      await act(async () => {
        result.current.history.onRetry();
        await Promise.resolve();
      });

      expect(sent(HISTORY_LIST)).toBe(sentBefore);
      expect(result.current.history.items).toEqual([]);
    });

    it('refetches when tapped while the switch is on', async () => {
      const { result } = await failHistoryLoad();
      const sentBefore = sent(HISTORY_LIST);

      await act(async () => {
        result.current.history.onRetry();
        await Promise.resolve();
      });

      await waitFor(() => expect(result.current.history.failed).toBe(false));
      expect(sent(HISTORY_LIST)).toBe(sentBefore + 1);
      expect(result.current.history.items.map((item) => item.query)).toEqual([QUERY]);
    });
  });
});
