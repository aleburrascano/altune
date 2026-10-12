import { QueryClient } from '@tanstack/react-query';
import { act, renderHook, waitFor } from '@testing-library/react-native';

import { useImpressionLogger } from '@features/discovery/hooks/useImpressionLogger';
import { resultFixture } from '@features/discovery/__tests__/fixtures';
import { makeWrapper } from '@testing/makeWrapper';
import { signInTestUser } from '@testing/signInTestUser';

import type { ViewToken } from 'react-native';
import type { DiscoverySearchResponse } from '@shared/apiClient/discovery';

const { __http } = require('@testing/doubles/fetch.js');

const EVENTS = 'POST /v1/discovery/events';
const EVENTS_PATH = '/v1/discovery/events';
const TWO_EVENTS = 2;

interface RecordedRequest {
  path: string;
  body: string;
}

interface RecordedEvent {
  type: string;
  search_id?: string;
  payload: Record<string, unknown>;
}

function sentEvents(): RecordedEvent[] {
  return (__http.requests as RecordedRequest[])
    .filter((request) => request.path === EVENTS_PATH)
    .map((request) => JSON.parse(request.body) as RecordedEvent);
}

async function settle(): Promise<void> {
  await act(async () => {
    await new Promise((resolve) => setTimeout(resolve, 0));
  });
}

let queryClient: QueryClient;
let wrapper: ReturnType<typeof makeWrapper>;

function responseFixture(
  overrides: Partial<DiscoverySearchResponse> = {},
): DiscoverySearchResponse {
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

beforeEach(async () => {
  queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  wrapper = makeWrapper(queryClient);
  await signInTestUser();
  __http.reply(EVENTS, { status: 204 });
});

afterEach(() => {
  __http.reset();
  queryClient.clear();
});

describe('useImpressionLogger emits a results_shown event once per search', () => {
  it('reports an item viewable only once it crosses the halfway visibility threshold', async () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()), { wrapper });

    expect(result.current.viewabilityConfig.itemVisiblePercentThreshold).toBe(50);
  });

  it('records the shown results with the search identity and projected rows', async () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()), { wrapper });

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    expect(sentEvents()[0]).toEqual({
      type: 'results_shown',
      search_id: 'search-1',
      payload: {
        session_id: expect.any(String),
        results: [
          { result_signature: 'sig', position: 0, provider: 'spotify', confidence: 'high' },
        ],
      },
    });
  });

  it('does not emit twice for the same search when items become viewable again', async () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()), { wrapper });

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));
    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await waitFor(() => expect(sentEvents()).toHaveLength(1));
    await settle();
    expect(sentEvents()).toHaveLength(1);
  });

  it('emits again once a new search produces a different search id', async () => {
    const { result, rerender } = renderHook(
      (props: DiscoverySearchResponse) => useImpressionLogger(props),
      { initialProps: responseFixture({ search_id: 'search-1' }), wrapper },
    );

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));
    rerender(responseFixture({ search_id: 'search-2' }));
    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await waitFor(() => expect(sentEvents()).toHaveLength(TWO_EVENTS));
  });

  it('does not emit when there is no search id', async () => {
    const { result } = renderHook(
      () => useImpressionLogger(responseFixture({ search_id: undefined })),
      { wrapper },
    );

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await settle();
    expect(sentEvents()).toEqual([]);
  });

  it('does not emit when nothing is viewable', async () => {
    const { result } = renderHook(() => useImpressionLogger(responseFixture()), { wrapper });

    act(() => result.current.onViewableItemsChanged({ viewableItems: [] }));

    await settle();
    expect(sentEvents()).toEqual([]);
  });

  it('does not emit when there are no results to report', async () => {
    const { result } = renderHook(
      () => useImpressionLogger(responseFixture({ results: [], total: 0 })),
      { wrapper },
    );

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await settle();
    expect(sentEvents()).toEqual([]);
  });

  it('does nothing when there is no search data at all', async () => {
    const { result } = renderHook(() => useImpressionLogger(undefined), { wrapper });

    act(() => result.current.onViewableItemsChanged({ viewableItems: visible }));

    await settle();
    expect(sentEvents()).toEqual([]);
  });
});
