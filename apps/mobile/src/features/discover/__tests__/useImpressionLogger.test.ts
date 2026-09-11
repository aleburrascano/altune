import { renderHook } from '@testing-library/react-native';

import { useImpressionLogger } from '../hooks/useImpressionLogger';

import type { ViewToken } from 'react-native';
import type { DiscoveryResult, DiscoverySearchResponse } from '@shared/api-client/discovery';

const mockMutate = jest.fn();

jest.mock('@shared/telemetry/useRecordEvent', () => ({
  useRecordEvent: () => ({ mutate: mockMutate }),
}));

function resultFixture(overrides: Partial<DiscoveryResult> = {}): DiscoveryResult {
  return {
    kind: 'track',
    title: 'The Title',
    subtitle: null,
    image_url: null,
    confidence: 'high',
    result_signature: 'sig',
    sources: [{ provider: 'spotify', external_id: 'ext-1', url: 'https://x' }],
    extras: {},
    ...overrides,
  };
}

function responseFixture(overrides: Partial<DiscoverySearchResponse> = {}): DiscoverySearchResponse {
  return {
    query: 'radiohead',
    query_norm: 'radiohead',
    search_id: 'search-1',
    results: [resultFixture()],
    sections: [],
    providers: [],
    partial: false,
    cache: { hit: false, fetched_at: null },
    total: 1,
    offset: 0,
    has_more: false,
    ...overrides,
  };
}

const visible = [{ item: {}, key: 'k', index: 0, isViewable: true }] as unknown as ViewToken[];

beforeEach(() => {
  mockMutate.mockClear();
});

describe('useImpressionLogger emits a results_shown event once per search', () => {
  it('reports an item viewable only once it crosses the halfway visibility threshold', () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()));

    expect(result.current.viewabilityConfig.itemVisiblePercentThreshold).toBe(50);
  });

  it('records the shown results with the search identity and projected rows', () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()));

    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).toHaveBeenCalledTimes(1);
    expect(mockMutate).toHaveBeenCalledWith({
      type: 'results_shown',
      query_norm: 'radiohead',
      search_id: 'search-1',
      payload: {
        results: [
          { result_signature: 'sig', position: 0, provider: 'spotify', confidence: 'high' },
        ],
      },
    });
  });

  it('does not emit twice for the same search when items become viewable again', () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()));

    result.current.onViewableItemsChanged({ viewableItems: visible });
    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).toHaveBeenCalledTimes(1);
  });

  it('emits again once a new search produces a different search id', () => {
    const { result, rerender } = renderHook(
      (props: DiscoverySearchResponse) => useImpressionLogger(props),
      { initialProps: responseFixture({ search_id: 'search-1' }) },
    );

    result.current.onViewableItemsChanged({ viewableItems: visible });
    rerender(responseFixture({ search_id: 'search-2' }));
    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).toHaveBeenCalledTimes(2);
  });

  it('does not emit when there is no search id', () => {
    const { result } = renderHook(() =>
      useImpressionLogger(responseFixture({ search_id: undefined })),
    );

    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).not.toHaveBeenCalled();
  });

  it('does not emit when nothing is viewable', () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()));

    result.current.onViewableItemsChanged({ viewableItems: [] });

    expect(mockMutate).not.toHaveBeenCalled();
  });

  it('does not emit when there are no results to report', () => {
    const { result } = renderHook(() =>
      useImpressionLogger(responseFixture({ results: [], total: 0 })),
    );

    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).not.toHaveBeenCalled();
  });

  it('does nothing when there is no search data at all', () => {
    const { result } = renderHook(() => useImpressionLogger(undefined));

    result.current.onViewableItemsChanged({ viewableItems: visible });

    expect(mockMutate).not.toHaveBeenCalled();
  });
});
