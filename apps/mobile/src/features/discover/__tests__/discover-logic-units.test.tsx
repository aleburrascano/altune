import React from 'react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';
import { Keyboard } from 'react-native';

import { clearSearchHistory } from '@shared/api-client/discovery';
import { discoveryKeys } from '@shared/lib/query-keys';
import { useClearSearchHistory } from '../hooks/useClearSearchHistory';
import { useResultTap } from '../hooks/useResultTap';
import { useResultsFilter } from '../hooks/useResultsFilter';
import { useSuggestionVisibility } from '../hooks/useSuggestionVisibility';
import { stashHandoffForDetail } from '../handoff';
import { resultFixture } from './fixtures';

import type { DiscoverySearchResponse } from '@shared/api-client/discovery';

const mockMutate = jest.fn();
const mockPush = jest.fn();

jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockMutate }),
}));
jest.mock('expo-router', () => ({
  useRouter: () => ({ push: mockPush }),
}));
jest.mock('@shared/api-client/discovery', () => ({
  clearSearchHistory: jest.fn(),
}));
jest.mock('../handoff', () => ({
  stashHandoffForDetail: jest.fn(() => '/discover/detail'),
}));

const mockClearSearchHistory = clearSearchHistory as jest.Mock;
const mockStash = stashHandoffForDetail as jest.Mock;

function responseFixture(
  overrides: Partial<DiscoverySearchResponse> = {},
): DiscoverySearchResponse {
  return {
    query: 'Radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture({ title: 'first' }), resultFixture({ title: 'second' })],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 2,
    offset: 0,
    has_more: false,
    ...overrides,
  };
}

beforeEach(() => {
  jest.clearAllMocks();
});

describe('useResultsFilter resets to "all" only when a new query is committed', () => {
  it('keeps the chosen filter while the query is unchanged and resets it on a new query', () => {
    const { result, rerender } = renderHook(
      ({ query }: { query: string }) => useResultsFilter(query),
      {
        initialProps: { query: 'radiohead' },
      },
    );

    act(() => result.current.setFilter('track'));
    rerender({ query: 'radiohead' });
    expect(result.current.filter).toBe('track');

    rerender({ query: 'bjork' });
    expect(result.current.filter).toBe('all');
  });
});

describe('useClearSearchHistory empties the cache at once and restores it on failure', () => {
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
    return { queryClient, invalidate, clear: result.current };
  }

  it('writes an empty history before the server call and does not refetch on success', async () => {
    mockClearSearchHistory.mockResolvedValue(undefined);
    const { queryClient, invalidate, clear } = setup();

    act(() => clear());

    expect(queryClient.getQueryData(discoveryKeys.history)).toEqual({ items: [] });
    await waitFor(() => expect(mockClearSearchHistory).toHaveBeenCalledTimes(1));
    expect(invalidate).not.toHaveBeenCalled();
  });

  it('invalidates the history query when the server call fails', async () => {
    mockClearSearchHistory.mockRejectedValue(new Error('offline'));
    const { invalidate, clear } = setup();

    act(() => clear());

    await waitFor(() =>
      expect(invalidate).toHaveBeenCalledWith({ queryKey: discoveryKeys.history }),
    );
  });
});

describe('useResultTap records result_clicked and hands off to the detail screen', () => {
  it('uses the global index, search identity and signature from the response', () => {
    const dismiss = jest.spyOn(Keyboard, 'dismiss');
    const data = responseFixture();
    const tapped = data.results[1]!;
    const { result } = renderHook(() => useResultTap(data, 'typed'));

    result.current(tapped, 0);

    expect(dismiss).toHaveBeenCalled();
    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      query_norm: 'radiohead',
      search_id: 'search-1',
      payload: {
        kind: 'track',
        title: 'second',
        subtitle: null,
        position: 1,
        confidence: 'high',
        provider: 'spotify',
        result_signature: 'sig',
      },
    });
    expect(mockStash).toHaveBeenCalledWith(tapped, 'search-1');
    expect(mockPush).toHaveBeenCalledWith('/discover/detail');
  });

  it('falls back to the committed query, the passed position, and omits a missing signature', () => {
    const orphan = resultFixture({ result_signature: undefined, sources: [] });
    const { result } = renderHook(() => useResultTap(undefined, 'typed'));

    result.current(orphan, 4);

    expect(mockMutate).toHaveBeenCalledWith({
      type: 'result_clicked',
      query_norm: 'typed',
      search_id: undefined,
      payload: {
        kind: 'track',
        title: 'The Title',
        subtitle: null,
        position: 4,
        confidence: 'high',
        provider: null,
      },
    });
  });
});

describe('useSuggestionVisibility shows suggestions only while the user is typing', () => {
  function setup(inputValue: string, count: number) {
    const search = {
      inputValue,
      onChangeText: jest.fn(),
      onSubmit: jest.fn(),
      setQuery: jest.fn(),
    };
    const hook = renderHook(({ n }: { n: number }) => useSuggestionVisibility(search, n), {
      initialProps: { n: count },
    });
    return { search, ...hook };
  }

  it('requires focus, two characters and at least one suggestion', () => {
    const { result, rerender } = setup('ra', 1);
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.setIsFocused(true));
    expect(result.current.showSuggestions).toBe(true);

    rerender({ n: 0 });
    expect(result.current.showSuggestions).toBe(false);

    expect(setup(' r ', 1).result.current.showSuggestions).toBe(false);
  });

  it('hides on submit and on suggestion select, and reopens on typing', () => {
    const { result, search } = setup('radio', 3);
    act(() => result.current.setIsFocused(true));

    act(() => result.current.onSubmit());
    expect(search.onSubmit).toHaveBeenCalled();
    expect(result.current.showSuggestions).toBe(false);

    act(() => result.current.onChangeText('radioh'));
    expect(search.onChangeText).toHaveBeenCalledWith('radioh');
    expect(result.current.showSuggestions).toBe(true);

    act(() => result.current.onSuggestionSelect('radiohead'));
    expect(search.setQuery).toHaveBeenCalledWith('radiohead');
    expect(result.current.showSuggestions).toBe(false);
  });
});
